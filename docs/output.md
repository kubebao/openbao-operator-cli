# Output and exit codes

## Formats

Every reporting command supports `--output text` (the default) and `--output json`. JSON is the stable, versioned
interface for scripts. Text output is for people and can change between releases.

Results go to standard output. Errors go to standard error.

## Text output

On a terminal, text output uses colour and these symbols:

| Symbol | Meaning |
| --- | --- |
| `✓` | Good |
| `!` | Warning |
| `✗` | Failure |
| `?` | Unknown |
| `•` | Note |

Select colour with `--color auto`, `always`, or `never`. `auto` honours `NO_COLOR`. Pipes, files, and JSON never
receive escape sequences.

`--version` has no short form. `-v` means `--verbose` on `status` and `doctor`.

## Exit codes

| Exit code | Meaning |
| --- | --- |
| `0` | The command completed. A status report can still describe an unhealthy cluster. |
| `1` | Invalid input, configuration error, missing cluster, ambiguous API group, or a rejected control. |
| `2` | `doctor` found a finding at or above `--fail-on` (default `warning`). |
| `3` | Evidence is incomplete, the cluster changed during the command, or a control outcome is unknown or not yet observed. |

Incomplete diagnosis takes precedence over findings in the exit code. The output still contains the confirmed
findings. `status` does not fail only because the cluster is unhealthy. A complete support bundle can contain serious
findings and still return `0`.
