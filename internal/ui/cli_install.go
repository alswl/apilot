package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alswl/apilot/pkg/config"
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

func (s *windowState) offerCLI() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if _, err := os.Lstat(filepath.Join(home, ".local", "bin", "apilot")); err == nil {
		return
	}
	if _, err := os.Stat(filepath.Join(config.DataDir(), "cli-introduction-seen")); err == nil {
		return
	}
	s.showCLI()
}

func (s *windowState) showCLI() {
	if s.cliWin != nil && !s.cliWin.IsDestroyed() {
		s.cliWin.Show()
		return
	}
	home, _ := os.UserHomeDir()
	target := filepath.Join(home, ".local", "bin", "apilot")
	source, sourceErr := bundledCLI()
	// App Translocation and mounted disk images are temporary installation paths.
	installed := strings.HasPrefix(source, "/Applications/") || strings.HasPrefix(source, filepath.Join(home, "Applications")+"/")
	message := ""
	if sourceErr != nil {
		message = sourceErr.Error()
	}
	pathCommand := `export PATH="$HOME/.local/bin:$PATH"`
	dismiss := func() {
		_ = os.WriteFile(filepath.Join(config.DataDir(), "cli-introduction-seen"), []byte("1\n"), 0600)
		s.cliWin.Close()
	}
	s.cliWin = mygo.NewWindow(mygo.WindowOptions{Title: tr("apilotCommandLineTool"), Width: 500, Height: 350, MinWidth: 440, MinHeight: 330, Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Column(c).Fill().Padding(20).Gap(14).Children(func() {
			ui.Row(c).FillWidth().Gap(12).Children(func() {
				ui.Image(c, appMark).Size(36, 36)
				ui.Text(c, tr("installCommandLineTool")).Bold().FontSize(20)
			})
			ui.Text(c, tr("manageTasksFromATerminalOrAgent")).TextColor(c.Theme().TextMuted)
			ui.Text(c, tr("installToLocalBinApilot")).Selectable()
			if !installed {
				ui.Text(c, tr("dragApilotToApplicationsThenReopenIt")).TextColor(c.Theme().Danger)
			}
			ui.Text(c, tr("ifApilotIsNotFoundAddThisLineToZshrc")).FontSize(12)
			ui.Text(c, pathCommand).Font("monospace").FontSize(12).Selectable()
			if message != "" {
				ui.Text(c, message).FontSize(12)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, tr("copyPath")).Clicked() {
					mygo.Clipboard.WriteText(pathCommand)
				}
				if ui.Button(c, tr("later")).Clicked() {
					dismiss()
				}
				if ui.PrimaryButton(c, tr("installCli")).Disabled(!installed || sourceErr != nil).Clicked() {
					if err := installCLI(source, target); err != nil {
						message = err.Error()
					} else {
						message = tr("installedRunApilotStatusToCheckTheConnection")
						_ = os.WriteFile(filepath.Join(config.DataDir(), "cli-introduction-seen"), []byte("1\n"), 0600)
					}
				}
			})
		})
	})})
	s.cliWin.OnClosed(func() {
		_ = os.WriteFile(filepath.Join(config.DataDir(), "cli-introduction-seen"), []byte("1\n"), 0600)
	})
}
