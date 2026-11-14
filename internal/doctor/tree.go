package doctor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

// ownershipTree shows UID-confirmed related resources under the selected cluster.
// Placement follows recorded owner UIDs only; names and labels never place a node.
func ownershipTree(snapshot evidence.Snapshot, verbose bool) string {
	related := snapshot.Related
	if related == nil || snapshot.Target.UID == "" {
		return ""
	}
	root := render.Node{Label: "OpenBaoCluster " + render.Bold(snapshot.Target.Name)}
	if verbose {
		root.Label += "  " + render.Faint("uid "+snapshot.Target.UID)
	}
	placed := map[string]bool{}
	for _, set := range related.StatefulSets {
		if set.Association.OwnerUID != snapshot.Target.UID {
			continue
		}
		node := render.Node{Label: "StatefulSet " + set.Name + "  " + statefulSetState(set)}
		for _, pod := range related.Pods {
			if pod.Association.OwnerUID == set.UID {
				node.Children = append(node.Children, podNode(pod, verbose))
				placed[pod.UID] = true
			}
		}
		root.Children = append(root.Children, node)
	}
	for _, pod := range related.Pods {
		if !placed[pod.UID] {
			root.Children = append(root.Children, podNode(pod, verbose))
		}
	}
	for _, restore := range related.Restores {
		label := "OpenBaoRestore " + restore.Name + "  " + restoreState(restore)
		if verbose && restore.Association.Basis != "" {
			label += "  " + render.Faint("via "+restore.Association.Basis)
		}
		root.Children = append(root.Children, render.Node{Label: label})
	}
	if len(root.Children) == 0 {
		root.Children = append(root.Children, render.Node{Label: render.Faint("no owned StatefulSets, Pods, or restores observed")})
	}
	if verbose && len(related.Omissions) > 0 {
		omitted := render.Node{Label: render.Paint(render.Unknown, "Unverified candidates") + "  " +
			render.Faint("matched by label or name; ownership not confirmed")}
		for _, o := range related.Omissions {
			omitted.Children = append(omitted.Children, render.Node{Label: fmt.Sprintf("%s %s  %s", o.Reference.Kind, o.Reference.Name,
				render.Faint(render.Humanize(o.Reason)))})
		}
		root.Children = append(root.Children, omitted)
	}
	return render.Tree(root, "  ")
}

func statefulSetState(set evidence.StatefulSet) string {
	parts := []string{}
	switch {
	case set.Deleting:
		parts = append(parts, render.Paint(render.Warn, "deleting"))
	case set.Ready != nil && set.Desired != nil:
		tone := render.Good
		if *set.Ready != *set.Desired {
			tone = render.Warn
		}
		parts = append(parts, render.Paint(tone, fmt.Sprintf("%d/%d ready", *set.Ready, *set.Desired)))
	default:
		parts = append(parts, render.Paint(render.Unknown, "readiness unknown"))
	}
	if set.Generation != nil && set.ObservedGeneration != nil && *set.Generation != *set.ObservedGeneration {
		parts = append(parts, render.Paint(render.Warn, "generation "+strconv.FormatInt(*set.Generation, 10)+" not yet observed"))
	}
	return strings.Join(parts, " · ")
}

func podNode(pod evidence.Pod, verbose bool) render.Node {
	state := []string{}
	if pod.Phase != "" {
		state = append(state, pod.Phase)
	}
	switch {
	case pod.Deleting:
		state = append(state, render.Paint(render.Warn, "terminating"))
	case pod.Ready == nil:
		state = append(state, render.Paint(render.Unknown, "readiness unknown"))
	case *pod.Ready:
		state = append(state, render.Paint(render.Good, "ready"))
	default:
		notReady := "not ready"
		if pod.ReadyReason != "" {
			notReady += " (" + pod.ReadyReason + ")"
		}
		state = append(state, render.Paint(render.Warn, notReady))
	}
	node := render.Node{Label: "Pod " + pod.Name + "  " + strings.Join(state, " · ")}
	if verbose {
		for _, c := range pod.Containers {
			node.Children = append(node.Children, render.Node{Label: containerState(c)})
		}
	}
	return node
}

func containerState(c evidence.ContainerState) string {
	parts := []string{}
	switch {
	case c.Ready == nil:
		parts = append(parts, render.Paint(render.Unknown, "readiness unknown"))
	case *c.Ready:
		parts = append(parts, render.Paint(render.Good, "ready"))
	default:
		parts = append(parts, render.Paint(render.Warn, "not ready"))
	}
	if c.RestartCount != nil {
		tone := render.Neutral
		if *c.RestartCount > 0 {
			tone = render.Warn
		}
		parts = append(parts, render.Paint(tone, fmt.Sprintf("%d restarts", *c.RestartCount)))
	}
	if c.WaitingReason != "" {
		parts = append(parts, render.Paint(render.Warn, "waiting: "+c.WaitingReason))
	}
	if c.TerminatedReason != "" {
		parts = append(parts, "last terminated: "+c.TerminatedReason)
	}
	kind := "container"
	if c.Kind == "init" {
		kind = "init container"
	}
	return render.Faint(kind) + " " + c.Name + "  " + strings.Join(parts, " · ")
}

func restoreState(restore evidence.Restore) string {
	phase := restore.Phase
	if phase == "" {
		phase = "phase not reported"
	}
	tone := render.Neutral
	switch phase {
	case "Completed":
		tone = render.Good
	case "Failed":
		tone = render.Bad
	case "Unknown":
		tone = render.Unknown
	}
	parts := []string{render.Paint(tone, phase)}
	if restore.ExecutionStage != "" && restore.ExecutionStage != phase {
		parts = append(parts, "stage "+restore.ExecutionStage)
	}
	return strings.Join(parts, " · ")
}
