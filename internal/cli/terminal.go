package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/kubebao/openbao-operator-cli/internal/render"
)

// terminal holds presentation settings shared by every command.
type terminal struct {
	color   string
	environ []string
}

func (t *terminal) mode() render.ColorMode {
	mode, err := render.ParseColorMode(t.color)
	if err != nil {
		return render.ColorAuto
	}
	return mode
}

func (t *terminal) stdout(command *cobra.Command) *render.Output {
	return render.NewOutput(command.OutOrStdout(), t.mode(), t.environ)
}

func (t *terminal) stderr(command *cobra.Command) *render.Output {
	return render.NewOutput(command.ErrOrStderr(), t.mode(), t.environ)
}

func (t *terminal) addFlags(root *cobra.Command) {
	root.PersistentFlags().StringVar(&t.color, "color", string(render.ColorAuto),
		"Use colour in text output: auto, always, or never; auto honours NO_COLOR")
	root.PersistentPreRunE = func(*cobra.Command, []string) error {
		_, err := render.ParseColorMode(t.color)
		return err
	}
}

// writeError reports a failure on standard error. Results that already produced a report
// (findings, incomplete evidence) get a short note instead of an error banner.
func (t *terminal) writeError(stderr io.Writer, err error) {
	output := render.NewOutput(stderr, t.mode(), t.environ)
	var result interface{ ExitCode() int }
	if errors.As(err, &result) && result.ExitCode() > 1 {
		_, _ = fmt.Fprintln(output, render.Faint("kubebao: "+err.Error()))
		return
	}
	_, _ = fmt.Fprintln(output, render.Paint(render.Bad, "Error:")+" "+err.Error())
}
