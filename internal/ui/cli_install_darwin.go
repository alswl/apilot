package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func bundledCLI() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	source := filepath.Join(filepath.Dir(exe), "..", "Resources", "bin", "apilot")
	source, err = filepath.Abs(source)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
		return "", fmt.Errorf("%s", tr("bundledCliNotFoundInstallTheCompleteApilotApp"))
	}
	return source, nil
}

// Never replace another CLI, executable or link. The App bundle stays the owner.
func installCLI(source, target string) error {
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			old, e := os.Readlink(target)
			if e == nil && old == source {
				return nil
			}
		}
		return fmt.Errorf(tr("sAlreadyExistsInspectAndMoveItBeforeInstalling"), target)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return os.Symlink(source, target)
}

func (s *windowState) showCLI() {
	s.settingsPage = 1
	s.showSettings()
}

type cliSettings struct {
	source, target, message string
	installed               bool
	sourceErr               error
}

func newCLISettings() *cliSettings {
	home, _ := os.UserHomeDir()
	source, err := bundledCLI()
	panel := &cliSettings{
		source: source, target: filepath.Join(home, ".local", "bin", "apilot"), sourceErr: err,
		installed: strings.HasPrefix(source, "/Applications/") || strings.HasPrefix(source, filepath.Join(home, "Applications")+"/"),
	}
	if err != nil {
		panel.message = err.Error()
	}
	return panel
}

func (panel *cliSettings) view(c *ui.Context) {
	pathCommand := `export PATH="$HOME/.local/bin:$PATH"`
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(14).Children(func() {
			ui.Text(c, tr("installCommandLineTool")).Bold().FontSize(17)
			ui.Text(c, tr("manageTasksFromATerminalOrAgent")).Wrap().TextColor(c.Theme().TextMuted)
			ui.Text(c, tr("installToLocalBinApilot")).Selectable()
			if !panel.installed {
				ui.Text(c, tr("dragApilotToApplicationsThenReopenIt")).Wrap().TextColor(c.Theme().Danger)
			}
			ui.Text(c, tr("ifApilotIsNotFoundAddThisLineToZshrc")).Wrap().FontSize(12)
			ui.Text(c, pathCommand).Wrap().Font("monospace").FontSize(12).Selectable()
			if panel.message != "" {
				ui.Text(c, panel.message).Wrap().FontSize(12)
			}
		})
	})
	ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, tr("copyPath")).Clicked() {
			mygo.Clipboard.WriteText(pathCommand)
		}
		if ui.PrimaryButton(c, tr("installCli")).Disabled(!panel.installed || panel.sourceErr != nil).Clicked() {
			if err := installCLI(panel.source, panel.target); err != nil {
				panel.message = err.Error()
			} else {
				panel.message = tr("installedRunApilotStatusToCheckTheConnection")
			}
		}
	})
}
