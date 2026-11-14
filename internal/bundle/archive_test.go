package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
	"github.com/kubebao/openbao-operator-cli/internal/doctor"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

func partialFixture() Bundle {
	at := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	return New(evidence.Snapshot{
		StartedAt: at, CompletedAt: at, ResultCode: "incomplete",
		Target:     evidence.Target{Context: "fixture", Namespace: "security", Name: "prod"},
		Collection: []evidence.Step{{Action: "discover", Outcome: "denied", CollectedAt: at}},
	}, doctor.Policy(at), buildinfo.Info{Version: "test"}, time.Second)
}

func TestPrivateAtomicPublicationAndOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.tar.gz")
	b := partialFixture()
	if err := Write(context.Background(), path, b, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("archive is not private")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	original, err := root.ReadFile("bundle.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(context.Background(), path, b, false); err == nil {
		t.Fatal("existing archive was overwritten")
	}
	after, err := root.ReadFile("bundle.tar.gz")
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("existing archive changed")
	}
	if _, err := Read(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	b.Manifest.CLI.Version = "replacement"
	if err := Write(context.Background(), path, b, true); err != nil {
		t.Fatal(err)
	}
	read, err := Read(context.Background(), path)
	if err != nil || read.Manifest.CLI.Version != "replacement" {
		t.Fatal("deliberate replacement did not publish")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal("temporary files remain")
	}
}

func TestConcurrentPublicationDoesNotReplace(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bundle.tar.gz")
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wait.Go(func() { results <- Write(context.Background(), path, partialFixture(), false) })
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("got %d publishers, want one", successes)
	}
}

func TestFailedOrCanceledWritePublishesNothing(t *testing.T) {
	t.Parallel()
	for _, invalid := range []bool{false, true} {
		dir := t.TempDir()
		b := partialFixture()
		ctx, cancel := context.WithCancel(context.Background())
		if invalid {
			b.Manifest.Policy.MaxEvidenceAge = 0
		} else {
			cancel()
		}
		err := Write(ctx, filepath.Join(dir, "bundle.tar.gz"), b, false)
		cancel()
		files, readErr := os.ReadDir(dir)
		if err == nil || readErr != nil || len(files) != 0 {
			t.Fatal("failed write left a public archive or temporary file")
		}
	}
}

func testArchive(t *testing.T, entries map[string][]byte, extra *tar.Header) []byte {
	t.Helper()
	var output bytes.Buffer
	zip := gzip.NewWriter(&output)
	archive := tar.NewWriter(zip)
	for _, name := range entryNames {
		data, present := entries[name]
		if !present {
			continue
		}
		if err := archive.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(data)), Mode: 0o600}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err := archive.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	// Oversized headers intentionally have no body, to check rejection before reads.
	if extra == nil || extra.Size == 0 {
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestRejectsUnsafeAndUnsupportedArchives(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		extra  *tar.Header
		change func(map[string][]byte)
	}{
		{name: "parent traversal", extra: &tar.Header{Name: "../PRIVATE_CANARY", Typeflag: tar.TypeReg}},
		{name: "absolute path", extra: &tar.Header{Name: "/PRIVATE_CANARY", Typeflag: tar.TypeReg}},
		{name: "symlink", extra: &tar.Header{Name: "manifest.json", Typeflag: tar.TypeSymlink, Linkname: "/PRIVATE_CANARY"}},
		{name: "hard link", extra: &tar.Header{Name: "manifest.json", Typeflag: tar.TypeLink, Linkname: "summary.txt"}},
		{name: "duplicate", extra: &tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg}},
		{name: "unknown entry", extra: &tar.Header{Name: "private.txt", Typeflag: tar.TypeReg}},
		{
			name: "oversized entry", change: func(e map[string][]byte) { delete(e, "manifest.json") },
			extra: &tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Size: 256<<10 + 1},
		},
		{name: "missing entry", change: func(e map[string][]byte) { delete(e, "summary.txt") }},
		{name: "future version", change: func(e map[string][]byte) {
			e["manifest.json"] = bytes.Replace(e["manifest.json"], []byte(`"formatVersion":"v3"`), []byte(`"formatVersion":"future"`), 1)
		}},
		{name: "old bundle format", change: func(e map[string][]byte) {
			e["manifest.json"] = bytes.Replace(e["manifest.json"],
				[]byte(`"formatVersion":"v3"`), []byte(`"formatVersion":"v1","evidenceSchemaVersion":"5"`), 1)
		}},
		{name: "removed evidence schema field", change: func(e map[string][]byte) {
			e["manifest.json"] = append([]byte(`{"evidenceSchemaVersion":"5",`), e["manifest.json"][1:]...)
		}},
		{name: "removed rule set field", change: func(e map[string][]byte) {
			e["manifest.json"] = bytes.Replace(e["manifest.json"], []byte(`"policy":{`), []byte(`"policy":{"ruleSetVersion":"9",`), 1)
		}},
		{name: "duplicate JSON key", change: func(e map[string][]byte) {
			e["manifest.json"] = bytes.Replace(
				e["manifest.json"],
				[]byte(`"formatVersion":`),
				[]byte(`"formatVersion":"PRIVATE_CANARY","formatVersion":`),
				1,
			)
		}},
		{name: "unknown JSON field", change: func(e map[string][]byte) {
			e["manifest.json"] = append([]byte(`{"PRIVATE_CANARY":true,`), e["manifest.json"][1:]...)
		}},
		{name: "invalid UTF8", change: func(e map[string][]byte) {
			e["manifest.json"] = bytes.Replace(e["manifest.json"], []byte(`"test"`), []byte{'"', 0xff, '"'}, 1)
		}},
		{name: "JSON nesting limit", change: func(e map[string][]byte) {
			e["evidence/capture.json"] = []byte(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entries, err := partialFixture().entries()
			if err != nil {
				t.Fatal(err)
			}
			if tc.change != nil {
				tc.change(entries)
			}
			_, err = readArchive(context.Background(), bytes.NewReader(testArchive(t, entries, tc.extra)))
			if err == nil || strings.Contains(err.Error(), "PRIVATE_CANARY") {
				t.Fatalf("unsafe archive accepted or source data exposed: %v", err)
			}
		})
	}
}

func TestArchiveIntegrityAndReplayIgnoresStoredReports(t *testing.T) {
	t.Parallel()
	entries, err := partialFixture().entries()
	if err != nil {
		t.Fatal(err)
	}
	entries["summary.txt"], entries["findings.json"] = []byte("PRIVATE_CANARY"), []byte("PRIVATE_CANARY")
	data := testArchive(t, entries, nil)
	b, err := readArchive(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(b.Snapshot())
	if bytes.Contains(snapshot, []byte("PRIVATE_CANARY")) {
		t.Fatal("stored report became analysis evidence")
	}
	corrupt := bytes.Clone(data)
	corrupt[len(corrupt)-8] ^= 0xff
	for _, data := range [][]byte{data[:len(data)-4], corrupt, append(bytes.Clone(data), data...)} {
		if _, err := readArchive(context.Background(), bytes.NewReader(data)); err == nil {
			t.Fatal("truncated, corrupt, or concatenated gzip accepted")
		}
	}
}

func TestRejectsCompressedAndExpandedSizeLimits(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "oversized-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Truncate(maxArchiveBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(context.Background(), path); err == nil {
		t.Fatal("compressed limit not enforced")
	}
	var bomb bytes.Buffer
	zip := gzip.NewWriter(&bomb)
	chunk := make([]byte, 1<<20)
	for range 97 {
		if _, err := zip.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readArchive(context.Background(), &bomb); err == nil {
		t.Fatal("expanded limit not enforced")
	}
}

func TestOldBundleFormatRequiresRecollection(t *testing.T) {
	t.Parallel()
	entries, err := partialFixture().entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, previous := range []string{`"formatVersion":"v1","evidenceSchemaVersion":"5"`, `"formatVersion":"v2"`} {
		current := maps.Clone(entries)
		current["manifest.json"] = bytes.Replace(entries["manifest.json"], []byte(`"formatVersion":"v3"`), []byte(previous), 1)
		_, err = readArchive(context.Background(), bytes.NewReader(testArchive(t, current, nil)))
		if err == nil || err.Error() != "unsupported bundle format; collect a new bundle with the current CLI" {
			t.Fatal("old bundle format did not produce recollection guidance")
		}
	}
}

func TestBundleUsesOneFormatMarker(t *testing.T) {
	t.Parallel()
	entries, err := partialFixture().entries()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range entries {
		if name == "summary.txt" {
			continue
		}
		for _, removed := range []string{
			"schemaVersion", "evidenceSchemaVersion", "ruleSetVersion", "adapterContract",
			"approvedContracts", "restoreUIDBindingContracts",
		} {
			if bytes.Contains(data, []byte(removed)) {
				t.Fatal("bundle contains removed version or projection metadata")
			}
		}
	}
	if bytes.Count(entries["manifest.json"], []byte(`"formatVersion"`)) != 1 ||
		!bytes.Contains(entries["manifest.json"], []byte(`"formatVersion":"v3"`)) {
		t.Fatal("bundle has no single current format marker")
	}
}
