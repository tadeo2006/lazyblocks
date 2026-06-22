package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jesseduffield/gocui"
	"github.com/tadeo2006/lazyblocks/internal/config"
	"github.com/tadeo2006/lazyblocks/internal/infrastructure/cron"
	"github.com/tadeo2006/lazyblocks/internal/infrastructure/docker"
	"github.com/tadeo2006/lazyblocks/internal/infrastructure/modrinth"
	"github.com/tadeo2006/lazyblocks/internal/infrastructure/rcon"
	"github.com/tadeo2006/lazyblocks/internal/infrastructure/storage"
)

type App struct {
	gui           *gocui.Gui
	cfg           *config.Config
	dockerAdapter *docker.Adapter
	configPath    string
	instance      config.Instance
	status        string
	logReader     io.ReadCloser
	isCreating    bool
	formOpen      bool
	currentTab    int
	currentFileDir string
}

func NewApp(cfg *config.Config, cfgPath string, adapter *docker.Adapter) (*App, error) {
	g, err := gocui.NewGui(gocui.NewGuiOpts{
		OutputMode:      gocui.Output256,
		SupportOverlaps: true,
	})
	if err != nil {
		return nil, err
	}
	
	app := &App{
		gui:           g,
		cfg:           cfg,
		configPath:    cfgPath,
		dockerAdapter: adapter,
		status:        "Unknown",
	}
	if len(cfg.Instances) > 0 {
		app.instance = cfg.Instances[0]
	}

	g.Highlight = true
	g.SelFrameColor = gocui.ColorGreen
	g.SelFgColor = gocui.ColorGreen

	g.SetManagerFunc(app.layout)
	g.InputEsc = true

	if err := app.keybindings(); err != nil {
		return nil, err
	}

	return app, nil
}

func (app *App) Run() error {
	defer app.gui.Close()

	go app.updateStatusLoop()
	go app.streamLogsLoop()

	return app.gui.MainLoop()
}

func (app *App) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Columna izquierda (45 chars)
	// 1. Estado
	if v, err := g.SetView("status", 0, 0, 45, 5, 0); err != nil {
		if err.Error() != "unknown view" { return err }
		v.Title = " Status "
		v.Wrap = true
		app.drawStatus(v)
	}

	remY := maxY - 6
	midY := 6 + (remY / 2)

	// 2. Menú Principal (Ahora arriba, de 6 a midY)
	if v, err := g.SetView("menu", 0, 6, 45, midY, 0); err != nil {
		if err.Error() != "unknown view" { return err }
		v.Title = " Server Actions "
		v.SelBgColor = gocui.ColorDefault
		v.SelFgColor = gocui.ColorWhite | gocui.AttrBold
		app.drawMenu(v)
	}

	// 3. Instancias (Ahora abajo, de midY+1 a maxY-2)
	if v, err := g.SetView("instances", 0, midY+1, 45, maxY-2, 0); err != nil {
		if err.Error() != "unknown view" { return err }
		v.Title = " Instances "
		v.SelBgColor = gocui.ColorDefault
		v.SelFgColor = gocui.ColorWhite | gocui.AttrBold
		app.drawInstances(v)
		g.SetCurrentView("instances") // Enfoque inicial
	}

	
	// Right: Logs and Tabs
	if v, err := g.SetView("main", 46, 0, maxX-1, maxY-2, 0); err != nil {
		if err.Error() != "unknown view" { return err }
		v.Autoscroll = true
		v.Wrap = true
		go app.streamLogsLoop()
	}

	// Tab Bar Overlay
	if v, err := g.SetView("tab_bar", 48, -1, maxX-2, 1, 0); err != nil {
		if err.Error() != "unknown view" { return err }
		v.Frame = false
		v.BgColor = gocui.ColorDefault
	}


	// Bottom-bar: Ayuda y controles
	if v, err := g.SetView("help", -1, maxY-2, maxX, maxY, 0); err != nil {
		if err.Error() != "unknown view" { return err }
		v.Frame = false
		v.BgColor = gocui.ColorWhite
		v.FgColor = gocui.ColorBlack
		fmt.Fprint(v, " <q> Quit   |   <tab> Switch Panel   |   <enter> Select / Focus Logs   |   <esc> Back / Unfocus Logs")
	}

	if len(app.cfg.Instances) == 0 && !app.isCreating && !app.formOpen {
		if v, err := g.SetView("welcome", maxX/2-30, maxY/2-4, maxX/2+30, maxY/2+5, 0); err != nil {
			if err.Error() != "unknown view" { return err }
			v.Title = " Welcome to LazyBlocks "

			wField := 0 // 0=Create, 1=Quit

			wCursor := func(i int) string {
				if i == wField { return ">" }
				return " "
			}

			drawWelcome := func() {
				v.Clear()
				fmt.Fprintln(v, "")
				fmt.Fprintln(v, "  You have no Minecraft instances yet.")
				fmt.Fprintln(v, "  Use the form below to create your first server.")
				fmt.Fprintln(v, "")
				fmt.Fprintf(v, " %s [ Create my first instance ]\n", wCursor(0))
				fmt.Fprintf(v, " %s [ Quit ]\n",                    wCursor(1))
			}
			drawWelcome()
			g.SetCurrentView("welcome")

			g.SetKeybinding("welcome", gocui.KeyArrowDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
				if wField < 1 { wField++ }
				drawWelcome()
				return nil
			})
			g.SetKeybinding("welcome", gocui.KeyArrowUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
				if wField > 0 { wField-- }
				drawWelcome()
				return nil
			})
			g.SetKeybinding("welcome", 'j', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
				if wField < 1 { wField++ }
				drawWelcome()
				return nil
			})
			g.SetKeybinding("welcome", 'k', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
				if wField > 0 { wField-- }
				drawWelcome()
				return nil
			})
			g.SetKeybinding("welcome", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
				if wField == 0 {
					g.DeleteView("welcome")
					app.formOpen = true
					app.showCreateInstanceForm(g)
				} else {
					return gocui.ErrQuit
				}
				return nil
			})
		}
	}

	app.drawTabBar(g)
	app.updateTabContent(g)
	app.updateHighlights(g)

	// Guarantee that active modals are always visible above tab content
	if current := g.CurrentView(); current != nil {
		name := current.Name()
		baseViews := map[string]bool{
			"status": true, "menu": true, "instances": true, "tab_bar": true, "help": true,
			"main": true, "tab_players": true, "tab_schedule": true, "tab_files": true, "tab_config": true,
			"instance_details": true,
		}
		if !baseViews[name] {
			g.SetViewOnTop(name)
		}
	}

	return nil
}

func (app *App) updateHighlights(g *gocui.Gui) {
	current := g.CurrentView()
	
	tabs := []string{"main", "tab_players", "tab_schedule", "tab_files", "tab_config"}
	rightPanelNames := append([]string{"instances", "menu"}, tabs...)
	
	for _, viewName := range rightPanelNames {
		v, err := g.View(viewName)
		if err == nil {
			if current != nil && current.Name() == viewName {
				v.Highlight = true
			} else {
				v.Highlight = false
			}
		}
	}
	
	if current != nil && current.Name() == "instances" {
		app.showInstanceDetails(g)
	} else {
		g.DeleteView("instance_details")
	}
}

func (app *App) drawInstances(v *gocui.View) {
	v.Clear()
	for _, inst := range app.cfg.Instances {
		prefix := " "
		if inst.ID == app.instance.ID {
			prefix = "*"
		}
		fmt.Fprintf(v, "[%s] %s\n", prefix, inst.Name)
	}
	fmt.Fprintln(v, " ")
	fmt.Fprintln(v, " [+] Create Instance")
	fmt.Fprintln(v, " [-] Delete Instance")
}

func (app *App) drawMenu(v *gocui.View) {
	v.Clear()
	fmt.Fprintln(v, " [>] Start Server")
	fmt.Fprintln(v, " [x] Stop Server")
	fmt.Fprintln(v, " [~] Restart Server")
	fmt.Fprintln(v, " [+] Server Configuration")
}

func (app *App) drawStatus(v *gocui.View) {
	v.Clear()
	fmt.Fprintf(v, "Nombre: %s\n", app.instance.Name)
	color := "\033[32m" // Green
	if app.status != "running" {
		color = "\033[31m" // Red
	}
	fmt.Fprintf(v, "Status: %s%s\033[0m\n", color, app.status)
	fmt.Fprintf(v, "RCON: %s:%d\n", app.instance.RCON.Host, app.instance.RCON.Port)
}

func (app *App) keybindings() error {
	g := app.gui

	// Quit
	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, app.quit); err != nil { return err }
	if err := g.SetKeybinding("", 'q', gocui.ModNone, app.quit); err != nil { return err }

	// Menu Navigation
	if err := g.SetKeybinding("menu", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown); err != nil { return err }
	if err := g.SetKeybinding("menu", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp); err != nil { return err }
	if err := g.SetKeybinding("menu", 'j', gocui.ModNone, app.cursorDown); err != nil { return err }
	if err := g.SetKeybinding("menu", 'k', gocui.ModNone, app.cursorUp); err != nil { return err }
	if err := g.SetKeybinding("menu", gocui.KeyEnter, gocui.ModNone, app.executeAction); err != nil { return err }

	// Tab to switch views
	if err := g.SetKeybinding("", gocui.KeyTab, gocui.ModNone, app.nextView); err != nil { return err }

	// Instances
	if err := g.SetKeybinding("instances", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown); err != nil { return err }
	if err := g.SetKeybinding("instances", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp); err != nil { return err }
	if err := g.SetKeybinding("instances", 'j', gocui.ModNone, app.cursorDown); err != nil { return err }
	if err := g.SetKeybinding("instances", 'k', gocui.ModNone, app.cursorUp); err != nil { return err }
	if err := g.SetKeybinding("instances", gocui.KeyEnter, gocui.ModNone, app.executeInstanceAction); err != nil { return err }

	// Esc to exit main view
	if err := g.SetKeybinding("main", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		g.SetCurrentView("menu")
		app.updateHighlights(g)
		return nil
	}); err != nil { return err }

	// Ctrl-C to quit globally
	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		return gocui.ErrQuit
	}); err != nil { return err }

	quitFunc := func(g *gocui.Gui, v *gocui.View) error {
		return gocui.ErrQuit
	}
	if err := g.SetKeybinding("instances", 'q', gocui.ModNone, quitFunc); err != nil { return err }
	if err := g.SetKeybinding("menu", 'q', gocui.ModNone, quitFunc); err != nil { return err }
	if err := g.SetKeybinding("main", 'q', gocui.ModNone, quitFunc); err != nil { return err }

	// Main view scrolling
	
	if err := g.SetKeybinding("main", gocui.KeyArrowRight, gocui.ModNone, app.nextTab); err != nil { return err }
	if err := g.SetKeybinding("main", gocui.KeyArrowLeft, gocui.ModNone, app.prevTab); err != nil { return err }

	if err := g.SetKeybinding("main", gocui.KeyArrowUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		v.Autoscroll = false
		ox, oy := v.Origin()
		if oy > 0 { v.SetOrigin(ox, oy-1) }
		return nil
	}); err != nil { return err }

	if err := g.SetKeybinding("main", gocui.KeyArrowDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		v.Autoscroll = false
		ox, oy := v.Origin()
		v.SetOrigin(ox, oy+1)
		return nil
	}); err != nil { return err }
	
	// Pressing 'a' enables autoscroll again
	if err := g.SetKeybinding("main", 'a', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		v.Autoscroll = true
		return nil
	}); err != nil { return err }

	// Mouse scrolling for main view
	if err := g.SetKeybinding("main", gocui.MouseWheelUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		v.Autoscroll = false
		ox, oy := v.Origin()
		if oy > 0 { v.SetOrigin(ox, oy-1) }
		return nil
	}); err != nil { return err }

	if err := g.SetKeybinding("main", gocui.MouseWheelDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		v.Autoscroll = false
		ox, oy := v.Origin()
		v.SetOrigin(ox, oy+1)
		return nil
	}); err != nil { return err }

	return nil
}

func (app *App) quit(g *gocui.Gui, v *gocui.View) error {
	if app.logReader != nil {
		app.logReader.Close()
	}
	return gocui.ErrQuit
}

func (app *App) cursorDown(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		cx, cy := v.Cursor()
		lines := strings.Split(v.Buffer(), "\n")

		// Avanzar hasta encontrar una línea con contenido
		nextY := cy + 1
		for nextY < len(lines)-1 && strings.TrimSpace(lines[nextY]) == "" {
			nextY++
		}

		if nextY < len(lines)-1 {
			if err := v.SetCursor(cx, nextY); err != nil {
				ox, oy := v.Origin()
				v.SetOrigin(ox, oy+(nextY-cy))
			}
			if v.Name() == "instances" {
				app.showInstanceDetails(g)
			}
		}
	}
	return nil
}

func (app *App) cursorUp(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ox, oy := v.Origin()
		cx, cy := v.Cursor()
		lines := strings.Split(v.Buffer(), "\n")

		// Retroceder hasta encontrar una línea con contenido
		nextY := cy - 1
		for nextY >= 0 && strings.TrimSpace(lines[nextY]) == "" {
			nextY--
		}

		if nextY >= 0 {
			if err := v.SetCursor(cx, nextY); err != nil && oy > 0 {
				v.SetOrigin(ox, oy-(cy-nextY))
			}
			if v.Name() == "instances" {
				app.showInstanceDetails(g)
			}
		}
	}
	return nil
}

func (app *App) nextView(g *gocui.Gui, v *gocui.View) error {
	current := g.CurrentView()
	if current == nil {
		g.SetCurrentView("instances")
		app.updateHighlights(g)
		return nil
	}

	tabs := []string{"main", "tab_players", "tab_schedule", "tab_files", "tab_config"}
	isTab := false
	for _, t := range tabs {
		if current.Name() == t { isTab = true; break }
	}

	if current.Name() != "instances" && current.Name() != "menu" && !isTab {
		return nil
	}

	switch {
	case current.Name() == "instances":
		g.SetCurrentView("menu")
	case current.Name() == "menu":
		g.SetCurrentView(tabs[app.currentTab])
	case isTab:
		g.SetCurrentView("instances")
	default:
		g.SetCurrentView("instances")
	}
	app.updateHighlights(g)
	return nil
}

func (app *App) executeInstanceAction(g *gocui.Gui, v *gocui.View) error {
	_, cy := v.Cursor()
	if cy < len(app.cfg.Instances) {
		app.showInstanceActionPrompt(g, app.cfg.Instances[cy])
	} else if cy == len(app.cfg.Instances)+1 { // [+] Create Instance
		app.showCreateInstanceForm(g)
	} else if cy == len(app.cfg.Instances)+2 { // [-] Delete Instance
		app.deleteCurrentInstance(g)
	}
	return nil
}

func (app *App) executeAction(g *gocui.Gui, v *gocui.View) error {
	_, cy := v.Cursor()
	ctx := context.Background()
	mainView, _ := g.View("main")

	switch cy {
	case 0: // Start
		fmt.Fprintln(mainView, "\n[ACTION] Preparing container to start...")
		go func() {
			err := app.dockerAdapter.CreateAndStart(ctx, app.instance, func(msg string) {
				app.gui.Update(func(g *gocui.Gui) error {
					fmt.Fprintf(mainView, "[DOCKER] %s\n", msg)
					return nil
				})
			})
			app.gui.Update(func(g *gocui.Gui) error {
				if err != nil {
					fmt.Fprintf(mainView, "[ERROR] Failed to start server: %v\n", err)
				} else {
					fmt.Fprintf(mainView, "[SUCCESS] The server is starting!\n")
				}
				app.updateStatus()
				if app.logReader == nil {
					go app.streamLogsLoop()
				}
				return nil
			})
		}()
	case 1: // Stop
		fmt.Fprintln(mainView, "\n[ACTION] Stopping container...")
		go func() {
			app.dockerAdapter.Stop(ctx, app.instance.ContainerName)
			app.updateStatus()
		}()
	case 2: // Restart
		fmt.Fprintln(mainView, "\n[ACTION] Restarting container...")
		go func() {
			app.dockerAdapter.Restart(ctx, app.instance.ContainerName)
			app.updateStatus()
		}()
	case 3: // Configuration
		app.showRamSelector(g, "menu", func(val string) {
			idx := -1
			for i, existing := range app.cfg.Instances {
				if existing.ID == app.instance.ID {
					idx = i
					break
				}
			}
			if idx != -1 {
				app.cfg.Instances[idx].Memory = val
				config.SaveConfig(app.configPath, app.cfg)
				app.instance.Memory = val
				mainView, _ := g.View("main")
				fmt.Fprintf(mainView, "\n[SYSTEM] Applying new RAM configuration: %s...\n", val)
				ctx := context.Background()
				go func() {
					app.dockerAdapter.Stop(ctx, app.instance.ContainerName)
					app.dockerAdapter.Remove(ctx, app.instance.ContainerName)
					err := app.dockerAdapter.CreateAndStart(ctx, app.cfg.Instances[idx], func(msg string) {})
					app.gui.Update(func(g *gocui.Gui) error {
						if err != nil {
							fmt.Fprintf(mainView, "[ERROR] %v\n", err)
						} else {
							fmt.Fprintf(mainView, "[SUCCESS] Container recreated with %s RAM.\n", val)
						}
						app.updateStatus()
						return nil
					})
				}()
			}
			g.SetCurrentView("menu")
			app.updateHighlights(g)
		})
	}
	return nil
}


func (app *App) processCreateInstance(name, mcVersion, ram, mrpackPath string) {
	id := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	dataDir := filepath.Join(os.Getenv("HOME"), "lazyblocks_data", id)
	os.MkdirAll(dataDir, os.ModePerm)

	mcType := "paper"
	finalVersion := mcVersion
	mainView, _ := app.gui.View("main")

	if mrpackPath != "" {
		fmt.Fprintf(mainView, "[MRPACK] Resolving modpack %s...\n", mrpackPath)
		localPath, err := modrinth.ResolveModpackPath(mrpackPath, os.TempDir())
		if err != nil {
			app.gui.Update(func(g *gocui.Gui) error {
				app.isCreating = false
				fmt.Fprintf(mainView, "\n[ERROR] Failed to resolve modpack: %v\n", err)
				return nil
			})
			return
		}

		fmt.Fprintf(mainView, "[MRPACK] Reading modpack %s...\n", localPath)
		info, err := modrinth.InstallMrPack(localPath, dataDir, func(file string, current int, total int) {
			app.gui.Update(func(g *gocui.Gui) error {
				fmt.Fprintf(mainView, "\r[MRPACK] Downloading mod %d/%d: %s", current, total, file)
				return nil
			})
		})
		
		if err != nil {
			app.gui.Update(func(g *gocui.Gui) error {
				app.isCreating = false
				fmt.Fprintf(mainView, "\n[ERROR] mrpack installation failed: %v\n", err)
				return nil
			})
			return
		}

		app.gui.Update(func(g *gocui.Gui) error {
			fmt.Fprintf(mainView, "\n[MRPACK] Modpack extracted! Type: %s, MC: %s\n", info.Type, info.MCVersion)
			return nil
		})
		mcType = info.Type
		finalVersion = info.MCVersion
	}

	if ram == "" {
		ram = "2G"
	}

	newInstance := config.Instance{
		ID:            id,
		Name:          name,
		Type:          mcType,
		MCVersion:     finalVersion,
		Memory:        ram,
		ContainerName: "mc-" + id,
		RCON: config.RCON{
			Enabled:     true,
			Host:        "127.0.0.1",
			Port:        25575 + len(app.cfg.Instances),
			PasswordEnv: "RCON_PASSWORD",
		},
		Paths: config.Paths{
			DataDir: dataDir,
		},
		Backup: config.Backup{Keep: 5},
	}
	app.cfg.Instances = append(app.cfg.Instances, newInstance)
	err := config.SaveConfig(app.configPath, app.cfg)

	app.gui.Update(func(g *gocui.Gui) error {
		app.isCreating = false
		if err != nil {
			fmt.Fprintf(mainView, "[ERROR] %v\n", err)
		} else {
			fmt.Fprintf(mainView, "[SUCCESS] Instancia '%s' creada y guardada.\n", name)
			
			if len(app.cfg.Instances) == 1 {
				app.instance = app.cfg.Instances[0]
			} else {
				app.instance = newInstance
			}
			
			app.status = "OFFLINE"
			if statusView, err := g.View("status"); err == nil {
				app.drawStatus(statusView)
			}
			if instView, err := g.View("instances"); err == nil {
				app.drawInstances(instView)
			}
			app.drawTabBar(g)
			
			if app.logReader != nil {
				app.logReader.Close()
			}
			go app.streamLogsLoop()
		}
		return nil
	})
}

func (app *App) showWorldsPrompt(g *gocui.Gui) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("worlds", maxX/2-30, maxY/2-4, maxX/2+30, maxY/2+5, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " Manage Worlds "
		v.Highlight = true
		v.SelBgColor = gocui.ColorDefault
		v.SelFgColor = gocui.ColorWhite | gocui.AttrBold

		fmt.Fprintln(v, " [+] Create new world")
		fmt.Fprintln(v, " [^] Import world")
		fmt.Fprintln(v, " [x] Delete current world")
		fmt.Fprintln(v, " [b] Backup current world")
		fmt.Fprintln(v, " [r] Restaurar un Backup")
		
		g.SetCurrentView("worlds")

		g.SetKeybinding("worlds", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("worlds")
			g.SetCurrentView("menu")
			return nil
		})

		g.SetKeybinding("worlds", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			_, cy := v.Cursor()
			g.DeleteView("worlds")
			g.SetCurrentView("menu")
			mainView, _ := g.View("main")
			
			switch cy {
			case 0: // Crear nuevo mundo
				fmt.Fprintf(mainView, "\n[SYSTEM] Deleting current world and creating a new one...\n")
				if err := storage.CreateEmptyWorld(app.instance.Paths.DataDir); err != nil {
					fmt.Fprintf(mainView, "[ERROR] %v\n", err)
				} else {
					fmt.Fprintf(mainView, "[SUCCESS] New clean world created.\n")
				}
			case 1: // Importar
				app.showImportPrompt(g)
			case 2: // Borrar
				fmt.Fprintf(mainView, "\n[SYSTEM] Deleting instance world...\n")
				if err := storage.DeleteWorld(app.instance.Paths.DataDir); err != nil {
					fmt.Fprintf(mainView, "[ERROR] %v\n", err)
				} else {
					fmt.Fprintf(mainView, "[SUCCESS] World deleted completely.\n")
				}
			case 3: // Respaldar
				fmt.Fprintf(mainView, "\n[BACKUP] Iniciando respaldo del mundo para la instancia '%s'...\n", app.instance.Name)
				go func() {
					outName, err := storage.BackupWorld(app.instance.Paths.DataDir, func(msg string) {
						app.gui.Update(func(g *gocui.Gui) error {
							fmt.Fprintf(mainView, "%s\n", msg)
							return nil
						})
					})
					app.gui.Update(func(g *gocui.Gui) error {
						if err != nil {
							fmt.Fprintf(mainView, "[BACKUP ERROR] %v\n", err)
						} else {
							fmt.Fprintf(mainView, "[BACKUP OK] Saved successfully to: %s\n", outName)
						}
						return nil
					})
				}()
			case 4: // Restaurar Backup
				app.showRestorePrompt(g)
			}
			return nil
		})

		// Flechas para el menú de mundos
		g.SetKeybinding("worlds", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding("worlds", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
		g.SetKeybinding("worlds", 'j', gocui.ModNone, app.cursorDown)
		g.SetKeybinding("worlds", 'k', gocui.ModNone, app.cursorUp)
	}
}

func (app *App) showImportPrompt(g *gocui.Gui) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("import_prompt", maxX/2-35, maxY/2-1, maxX/2+35, maxY/2+1, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " Absolute path of the .zip or .tar.gz file to import "
		v.Editable = true
		g.SetCurrentView("import_prompt")

		g.SetKeybinding("import_prompt", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("import_prompt")
			g.SetCurrentView("menu")
			return nil
		})

		g.SetKeybinding("import_prompt", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			path := strings.TrimSpace(v.Buffer())
			g.DeleteView("import_prompt")
			g.SetCurrentView("menu")
			
			if path != "" {
				mainView, _ := g.View("main")
				fmt.Fprintf(mainView, "\n[IMPORT] Extrayendo %s...\n", path)
				go func() {
					err := storage.ImportWorld(app.instance.Paths.DataDir, path, func(msg string) {
						app.gui.Update(func(g *gocui.Gui) error {
							fmt.Fprintf(mainView, "%s\n", msg)
							return nil
						})
					})
					app.gui.Update(func(g *gocui.Gui) error {
						if err != nil {
							fmt.Fprintf(mainView, "[IMPORT ERROR] %v\n", err)
						} else {
							fmt.Fprintf(mainView, "[IMPORT OK] World imported successfully.\n")
						}
						return nil
					})
				}()
			}
			return nil
		})
	}
}

func (app *App) showRconPrompt(g *gocui.Gui) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("rcon", maxX/2-30, maxY/2-1, maxX/2+30, maxY/2+1, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " Comando RCON (Enter para enviar, Esc para cancelar) "
		v.Editable = true
		g.SetCurrentView("rcon")

		g.SetKeybinding("rcon", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("rcon")
			g.SetCurrentView("menu")
			return nil
		})

		g.SetKeybinding("rcon", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			cmdStr := strings.TrimSpace(v.Buffer())
			g.DeleteView("rcon")
			g.SetCurrentView("menu")
			if cmdStr != "" {
				app.runRcon(cmdStr)
			}
			return nil
		})
	}
}

func (app *App) runRcon(cmdStr string) {
	mainView, _ := app.gui.View("main")
	fmt.Fprintf(mainView, "\n[RCON] > %s\n", cmdStr)
	
	go func() {
		password := os.Getenv(app.instance.RCON.PasswordEnv)
		if password == "" { password = "secret-dev-password" }
		client, err := rcon.Dial(app.instance.RCON.Host, app.instance.RCON.Port, password)
		if err != nil {
			app.gui.Update(func(g *gocui.Gui) error {
				fmt.Fprintf(mainView, "[RCON ERROR] %v\n", err)
				return nil
			})
			return
		}
		defer client.Close()

		output, err := client.Execute(cmdStr)
		app.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				fmt.Fprintf(mainView, "[RCON ERROR] %v\n", err)
			} else {
				fmt.Fprintf(mainView, "[RCON RESP] %s\n", strings.TrimSpace(output))
			}
			return nil
		})
	}()
}

func (app *App) updateStatus() {
	status, err := app.dockerAdapter.GetStatus(context.Background(), app.instance.ContainerName)
	if err == nil {
		app.gui.Update(func(g *gocui.Gui) error {
			app.status = status
			return nil
		})
	}
}

func (app *App) updateStatusLoop() {
	for {
		app.updateStatus()
		time.Sleep(3 * time.Second)
	}
}

func (app *App) streamLogsLoop() {
	// Don't try to stream logs if no instance is configured
	if app.instance.ContainerName == "" {
		return
	}
	reader, err := app.dockerAdapter.StreamLogs(context.Background(), app.instance.ContainerName, "100")
	if err != nil {
		// Only report error if a container is actually expected to exist
		if app.status == "RUNNING" || app.status == "STOPPED" {
			errLog := err
			app.gui.Update(func(g *gocui.Gui) error {
				if v, err := g.View("main"); err == nil {
					fmt.Fprintf(v, "Error reading logs: %v\n", errLog)
				}
				return nil
			})
		}
		return
	}
	app.logReader = reader

	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			app.gui.Update(func(g *gocui.Gui) error {
				if v, err := g.View("main"); err == nil {
					fmt.Fprint(v, chunk)
				}
				return nil
			})
		}
		if err != nil {
			break
		}
	}
}

func (app *App) showCronPrompt(g *gocui.Gui) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("cron_prompt", maxX/2-40, maxY/2-2, maxX/2+40, maxY/2+2, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = fmt.Sprintf(" Backups Automáticos: %s ", app.instance.Name)
		v.Editable = true
		fmt.Fprintln(v, "¿Cada cuántas horas hacer un backup? (0 para desactivar):")
		v.SetCursor(0, 1)
		g.SetCurrentView("cron_prompt")

		g.SetKeybinding("cron_prompt", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("cron_prompt")
			g.SetCurrentView("menu")
			return nil
		})

		g.SetKeybinding("cron_prompt", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			lines := strings.Split(v.Buffer(), "\n")
			hoursStr := "0"
			if len(lines) > 1 {
				hoursStr = strings.TrimSpace(lines[1])
			}
			hours, _ := strconv.Atoi(hoursStr)
			
			g.DeleteView("cron_prompt")
			g.SetCurrentView("menu")
			
			mainView, _ := g.View("main")
			binPath, _ := os.Executable()

			if hours > 0 {
				err := cron.ScheduleBackup(app.instance.ID, binPath, hours)
				if err != nil {
					fmt.Fprintf(mainView, "\n[ERROR] Failed to schedule backups: %v\n", err)
				} else {
					fmt.Fprintf(mainView, "\n[SYSTEM] Backups programados cada %d horas.\n", hours)
				}
			} else {
				err := cron.RemoveBackup(app.instance.ID)
				if err != nil {
					fmt.Fprintf(mainView, "\n[ERROR] Failed to disable backups: %v\n", err)
				} else {
					fmt.Fprintf(mainView, "\n[SYSTEM] Backups automáticos desactivados.\n")
				}
			}
			return nil
		})
	}
}

func (app *App) showRamPrompt(g *gocui.Gui) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("ram_prompt", maxX/2-40, maxY/2-2, maxX/2+40, maxY/2+2, 0); err != nil {
		if err.Error() != "unknown view" { return }
		
		currentRam := app.instance.Memory
		if currentRam == "" {
			currentRam = "Por Defecto (1G)"
		}

		v.Title = fmt.Sprintf(" Configure RAM: %s (Current: %s) ", app.instance.Name, currentRam)
		v.Editable = true
		fmt.Fprintln(v, "Enter the new RAM amount (e.g., 2G, 4000M) or leave blank to reset:")
		v.SetCursor(0, 1)
		g.SetCurrentView("ram_prompt")

		g.SetKeybinding("ram_prompt", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("ram_prompt")
			g.SetCurrentView("menu")
			return nil
		})

		g.SetKeybinding("ram_prompt", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			lines := strings.Split(v.Buffer(), "\n")
			newRam := ""
			if len(lines) > 1 {
				newRam = strings.TrimSpace(lines[1])
			}
			
			g.DeleteView("ram_prompt")
			g.SetCurrentView("menu")
			
			// Update config
			app.instance.Memory = newRam
			
			// Update array
			for i, inst := range app.cfg.Instances {
				if inst.ID == app.instance.ID {
					app.cfg.Instances[i].Memory = newRam
					break
				}
			}
			config.SaveConfig(app.configPath, app.cfg)

			mainView, _ := g.View("main")
			fmt.Fprintf(mainView, "\n[SYSTEM] Applying new RAM configuration: %s...\n", newRam)
			
			// Remove the existing container so it gets recreated on next Start
			go func() {
				ctx := context.Background()
				app.dockerAdapter.Stop(ctx, app.instance.ContainerName)
				app.dockerAdapter.Remove(ctx, app.instance.ContainerName)
				
				app.gui.Update(func(g *gocui.Gui) error {
					fmt.Fprintf(mainView, "[SYSTEM] Contenedor destruido. La nueva RAM se aplicará cuando presiones 'Iniciar Servidor'.\n")
					app.updateStatus()
					return nil
				})
			}()

			return nil
		})
	}
}

type PropItem struct {
	Key string
	Val string
	IsBool bool
}



func (app *App) showPropInput(g *gocui.Gui, item *PropItem, returnView string, onComplete func()) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("prop_input", maxX/2-30, maxY/2-1, maxX/2+30, maxY/2+2, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = fmt.Sprintf(" Edit: %s ", item.Key)
		v.Editable = true
		fmt.Fprintln(v, item.Val)
		v.SetCursor(0, 1)
		g.SetCurrentView("prop_input")

		g.SetKeybinding("prop_input", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("prop_input")
			g.SetCurrentView(returnView)
			app.updateHighlights(g)
			return nil
		})

		g.SetKeybinding("prop_input", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			lines := strings.Split(v.Buffer(), "\n")
			if len(lines) > 1 {
				item.Val = strings.TrimSpace(lines[1])
			}
			g.DeleteView("prop_input")
			g.SetCurrentView(returnView)
			app.updateHighlights(g)
			onComplete()
			return nil
		})
	}
}

func (app *App) showRestorePrompt(g *gocui.Gui) {
	maxX, maxY := g.Size()
	backups, err := storage.ListBackups(app.instance.Paths.DataDir)
	if err != nil || len(backups) == 0 {
		mainView, _ := g.View("main")
		fmt.Fprintf(mainView, "\n[SYSTEM] No backups available for this instance.\n")
		g.SetCurrentView("menu")
		return
	}

	if v, err := g.SetView("restore_list", maxX/2-30, maxY/2-6, maxX/2+30, maxY/2+6, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " Select Backup to Restore "
		v.Highlight = true
		v.SelBgColor = gocui.ColorDefault
		v.SelFgColor = gocui.ColorWhite | gocui.AttrBold

		for _, b := range backups {
			fmt.Fprintln(v, " "+b)
		}
		
		g.SetCurrentView("restore_list")

		g.SetKeybinding("restore_list", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("restore_list")
			g.SetCurrentView("menu")
			return nil
		})

		g.SetKeybinding("restore_list", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			_, cy := v.Cursor()
			if cy >= len(backups) {
				return nil
			}
			selectedBackup := backups[cy]

			g.DeleteView("restore_list")
			g.SetCurrentView("menu")

			mainView, _ := g.View("main")
			fmt.Fprintf(mainView, "\n[RESTORE] Restaurando backup: %s\n", selectedBackup)
			
			go func() {
				ctx := context.Background()
				app.dockerAdapter.Stop(ctx, app.instance.ContainerName)

				err := storage.RestoreWorld(app.instance.Paths.DataDir, selectedBackup, func(msg string) {
					app.gui.Update(func(g *gocui.Gui) error {
						fmt.Fprintf(mainView, "%s\n", msg)
						return nil
					})
				})

				app.gui.Update(func(g *gocui.Gui) error {
					if err != nil {
						fmt.Fprintf(mainView, "[RESTORE ERROR] %v\n", err)
					} else {
						fmt.Fprintf(mainView, "[RESTORE OK] World restored. Press 'Start Server'.\n")
					}
					return nil
				})
			}()

			return nil
		})

		g.SetKeybinding("restore_list", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding("restore_list", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
		g.SetKeybinding("restore_list", 'j', gocui.ModNone, app.cursorDown)
		g.SetKeybinding("restore_list", 'k', gocui.ModNone, app.cursorUp)
	}
}

func (app *App) showModSearchPrompt(g *gocui.Gui) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("mod_search", maxX/2-30, maxY/2-1, maxX/2+30, maxY/2+2, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " Search Plugin / Mod on Modrinth "
		v.Editable = true
		v.SetCursor(0, 0)
		g.SetCurrentView("mod_search")

		g.SetKeybinding("mod_search", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("mod_search")
			g.SetCurrentView("menu")
			return nil
		})

		g.SetKeybinding("mod_search", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			query := strings.TrimSpace(v.Buffer())
			if query == "" { return nil }

			g.DeleteView("mod_search")
			g.SetCurrentView("menu")

			mainView, _ := g.View("main")
			fmt.Fprintf(mainView, "\n[SYSTEM] Buscando '%s' en Modrinth...\n", query)

			go func() {
				res, err := modrinth.Search(query, 10)
				app.gui.Update(func(g *gocui.Gui) error {
					if err != nil {
						fmt.Fprintf(mainView, "[ERROR] %v\n", err)
						return nil
					}
					if len(res.Hits) == 0 {
						fmt.Fprintf(mainView, "[SYSTEM] No se encontraron resultados.\n")
						return nil
					}
					app.showModResults(g, res)
					return nil
				})
			}()

			return nil
		})
	}
}

func (app *App) showModResults(g *gocui.Gui, res *modrinth.SearchResult) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("mod_results", maxX/2-35, maxY/2-6, maxX/2+35, maxY/2+6, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " Selecciona el Plugin/Mod para Instalar "
		v.Highlight = true
		v.SelBgColor = gocui.ColorDefault
		v.SelFgColor = gocui.ColorWhite | gocui.AttrBold

		for _, hit := range res.Hits {
			// Title - Type
			fmt.Fprintf(v, " %s (%s)\n", hit.Title, hit.ProjectType)
		}

		g.SetCurrentView("mod_results")

		g.SetKeybinding("mod_results", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("mod_results")
			g.SetCurrentView("menu")
			return nil
		})

		g.SetKeybinding("mod_results", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			_, cy := v.Cursor()
			if cy >= len(res.Hits) { return nil }
			hit := res.Hits[cy]

			g.DeleteView("mod_results")
			g.SetCurrentView("menu")

			mainView, _ := g.View("main")
			fmt.Fprintf(mainView, "\n[MODRINTH] Starting download of: %s\n", hit.Title)

			go func() {
				err := modrinth.DownloadLatest(hit.ProjectID, hit.ProjectType, app.instance.Paths.DataDir, func(msg string) {
					app.gui.Update(func(g *gocui.Gui) error {
						fmt.Fprintf(mainView, "%s\n", msg)
						return nil
					})
				})

				app.gui.Update(func(g *gocui.Gui) error {
					if err != nil {
						fmt.Fprintf(mainView, "[ERROR] %v\n", err)
					}
					return nil
				})
			}()

			return nil
		})

		g.SetKeybinding("mod_results", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding("mod_results", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
		g.SetKeybinding("mod_results", 'j', gocui.ModNone, app.cursorDown)
		g.SetKeybinding("mod_results", 'k', gocui.ModNone, app.cursorUp)
	}
}

func (app *App) deleteCurrentInstance(g *gocui.Gui) {
	if len(app.cfg.Instances) == 0 { return }
	
	mainView, _ := g.View("main")
	fmt.Fprintf(mainView, "\n[SYSTEM] Deleting instance '%s'...\n", app.instance.Name)
	
	ctx := context.Background()
	app.dockerAdapter.Stop(ctx, app.instance.ContainerName)
	app.dockerAdapter.Remove(ctx, app.instance.ContainerName)
	
	os.RemoveAll(app.instance.Paths.DataDir)
	
	idx := -1
	for i, inst := range app.cfg.Instances {
		if inst.ID == app.instance.ID {
			idx = i
			break
		}
	}
	if idx != -1 {
		app.cfg.Instances = append(app.cfg.Instances[:idx], app.cfg.Instances[idx+1:]...)
	}
	config.SaveConfig(app.configPath, app.cfg)
	
	if len(app.cfg.Instances) > 0 {
		app.instance = app.cfg.Instances[0]
	} else {
		app.instance = config.Instance{}
	}
	
	app.gui.Update(func(g *gocui.Gui) error {
		if v, err := g.View("instances"); err == nil {
			app.drawInstances(v)
		}
		if v, err := g.View("status"); err == nil {
			app.drawStatus(v)
		}
		if v, err := g.View("main"); err == nil {
			v.Title = " Logs "
		}
		fmt.Fprintf(mainView, "[SUCCESS] Instance deleted.\n")
		return nil
	})
}

func (app *App) showCreateInstanceForm(g *gocui.Gui) {
	maxX, maxY := g.Size()
	
	width := 82
	height := 16
	x0 := (maxX - width) / 2
	y0 := (maxY - height) / 2
	
	vName := fmt.Sprintf("create_form_%d", time.Now().UnixNano())
	
	if v, err := g.SetView(vName, x0, y0, x0+width, y0+height, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " New Instance Wizard "

		// Field index: 0=Name, 1=MCVersion, 2=RAM, 3=Modrinth, 4=LocalFile, 5=Confirm, 6=Cancel
		curField := 0
		numFields := 7
		
		form := &struct{
			Name string
			MCVersion string
			Ram string
			Modrinth string
			LocalPack string
		}{
			Name: "New Server",
			MCVersion: "1.20.4",
			Ram: "2G",
			Modrinth: "",
			LocalPack: "",
		}

		cursor := func(i int) string {
			if i == curField { return ">" }
			return " "
		}

		drawForm := func() {
			v.Clear()
			fmt.Fprintln(v, "  Configure your new Minecraft server:")
			fmt.Fprintln(v, "")
			fmt.Fprintf(v, " %s [Name]       %-22s  Instance name\n",        cursor(0), form.Name)
			fmt.Fprintf(v, " %s [MC Version] %-22s  Game version (e.g. 1.20.4)\n", cursor(1), form.MCVersion)
			fmt.Fprintf(v, " %s [RAM]        %-22s  Memory (e.g. 2G, 4G)\n",       cursor(2), form.Ram)
			fmt.Fprintln(v, "")
			fmt.Fprintln(v, "  Modpack (optional, choose one):")
			fmt.Fprintf(v, " %s [Modrinth]   %-22s  Slug or modrinth.com URL\n",    cursor(3), form.Modrinth)
			fmt.Fprintf(v, " %s [Local File] %-22s  Browse .mrpack file\n",         cursor(4), form.LocalPack)
			fmt.Fprintln(v, "")
			fmt.Fprintf(v, " %s [ Confirm and Create ]\n", cursor(5))
			fmt.Fprintf(v, " %s [ Cancel ]\n",             cursor(6))
		}
		drawForm()
		
		g.SetCurrentView(vName)

		g.SetKeybinding(vName, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView(vName)
			app.formOpen = false
			g.SetCurrentView("instances")
			return nil
		})

		g.SetKeybinding(vName, gocui.KeyArrowDown, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			if curField < numFields-1 { curField++ }
			drawForm()
			return nil
		})
		g.SetKeybinding(vName, gocui.KeyArrowUp, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			if curField > 0 { curField-- }
			drawForm()
			return nil
		})
		g.SetKeybinding(vName, 'j', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			if curField < numFields-1 { curField++ }
			drawForm()
			return nil
		})
		g.SetKeybinding(vName, 'k', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			if curField > 0 { curField-- }
			drawForm()
			return nil
		})

		g.SetKeybinding(vName, gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			switch curField {
			case 0:
				app.showFormInput(g, "Instance Name", form.Name, vName, func(val string) { form.Name = val; drawForm() })
			case 1:
				app.showFormInput(g, "Minecraft Version", form.MCVersion, vName, func(val string) { form.MCVersion = val; drawForm() })
			case 2:
				app.showRamSelector(g, vName, func(val string) { form.Ram = val; drawForm() })
			case 3:
				app.showFormInput(g, "Modrinth Slug or URL", form.Modrinth, vName, func(val string) {
					form.Modrinth = val
					form.LocalPack = ""
					drawForm()
				})
			case 4:
				app.showFileExplorer(g, "", vName, func(val string) {
					form.LocalPack = val
					form.Modrinth = ""
					drawForm()
				})
			case 5: // Confirm
				g.DeleteView(vName)
				app.formOpen = false
				app.isCreating = true
				if mainView, err := g.View("main"); err == nil {
					mainView.Clear()
				}
				g.SetCurrentView("main")
				app.updateHighlights(g)
				mrpackPath := form.Modrinth
				if form.LocalPack != "" {
					mrpackPath = form.LocalPack
				}
				go app.processCreateInstance(form.Name, form.MCVersion, form.Ram, mrpackPath)
			case 6: // Cancel
				g.DeleteView(vName)
				app.formOpen = false
				g.SetCurrentView("instances")
			}
			return nil
		})
	}
}

func (app *App) showRamSelector(g *gocui.Gui, returnView string, onSelect func(string)) {
	maxX, maxY := g.Size()
	vName := "ram_selector_" + returnView // unique name
	if v, err := g.SetView(vName, maxX/2-20, maxY/2-4, maxX/2+20, maxY/2+4, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " Select RAM "
		v.Highlight = true
		v.SelBgColor = gocui.ColorDefault
		v.SelFgColor = gocui.ColorWhite | gocui.AttrBold

		options := []string{"1G", "2G", "4G", "6G", "8G", "Other..."}
		
		for _, opt := range options {
			fmt.Fprintf(v, " %s\n", opt)
		}
		
		g.SetCurrentView(vName)

		g.SetKeybinding(vName, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView(vName)
			g.SetCurrentView(returnView)
			app.updateHighlights(g)
			return nil
		})

		g.SetKeybinding(vName, gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			_, cy := v.Cursor()
			if cy >= 0 && cy < len(options) {
				g.DeleteView(vName)
				if options[cy] == "Other..." {
					app.showFormInput(g, "Custom RAM (e.g., 3G)", "", returnView, onSelect)
				} else {
					g.SetCurrentView(returnView)
					app.updateHighlights(g)
					onSelect(options[cy])
				}
			}
			return nil
		})

		g.SetKeybinding(vName, gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding(vName, gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
		g.SetKeybinding(vName, 'j', gocui.ModNone, app.cursorDown)
		g.SetKeybinding(vName, 'k', gocui.ModNone, app.cursorUp)
		g.SetKeybinding(vName, gocui.MouseWheelDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding(vName, gocui.MouseWheelUp, gocui.ModNone, app.cursorUp)
	}
}

func (app *App) showFormInput(g *gocui.Gui, title, current, returnView string, onComplete func(string)) {
	maxX, maxY := g.Size()
	
	// Create a unique view name to prevent gocui keybinding collisions
	vName := "form_input_" + strings.ReplaceAll(title, " ", "_")
	
	if v, err := g.SetView(vName, maxX/2-30, maxY/2-1, maxX/2+30, maxY/2+1, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = " " + title + " "
		v.Editable = true
		fmt.Fprint(v, current)
		v.SetCursor(len(current), 0)
		g.SetCurrentView(vName)

		g.SetKeybinding(vName, gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView(vName)
			g.SetCurrentView(returnView)
			app.updateHighlights(g)
			return nil
		})

		g.SetKeybinding(vName, gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			val := strings.TrimSpace(v.Buffer())
			g.DeleteView(vName)
			g.SetCurrentView(returnView)
			app.updateHighlights(g)
			onComplete(val)
			return nil
		})
	}
}

func (app *App) showFileExplorer(g *gocui.Gui, currentDir, returnView string, onSelect func(string)) {
	if currentDir == "" {
		currentDir = filepath.Join(os.Getenv("HOME"), "lazyblocks_data", "modpacks")
		os.MkdirAll(currentDir, os.ModePerm)
	}
	
	maxX, maxY := g.Size()
	if v, err := g.SetView("explorer", maxX/2-40, maxY/2-10, maxX/2+40, maxY/2+10, 0); err != nil {
		if err.Error() != "unknown view" { return }
		
		v.Highlight = true
		v.SelBgColor = gocui.ColorDefault
		v.SelFgColor = gocui.ColorWhite | gocui.AttrBold

		var files []os.DirEntry
		
		drawExplorer := func(dir string) {
			v.Clear()
			v.Title = " Explorer: " + dir + " "
			
			entries, err := os.ReadDir(dir)
			if err != nil {
				fmt.Fprintln(v, " [ERROR] Could not read directory")
				return
			}
			
			files = []os.DirEntry{}
			fmt.Fprintln(v, " [X] No Modpack (Leave Empty)")
			fmt.Fprintln(v, " [DIR] .. (Go up one level)")
			for _, e := range entries {
				if e.IsDir() || strings.HasSuffix(e.Name(), ".mrpack") || strings.HasSuffix(e.Name(), ".zip") {
					files = append(files, e)
					icon := "[FILE]"
					if e.IsDir() { icon = "[DIR]" }
					fmt.Fprintf(v, " %s %s\n", icon, e.Name())
				}
			}
			v.SetCursor(0, 0)
			v.SetOrigin(0, 0)
		}
		
		drawExplorer(currentDir)
		g.SetCurrentView("explorer")

		g.SetKeybinding("explorer", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("explorer")
			g.SetCurrentView(returnView)
			app.updateHighlights(g)
			return nil
		})

		g.SetKeybinding("explorer", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			_, cy := v.Cursor()
			
			if cy == 0 {
				g.DeleteView("explorer")
				g.SetCurrentView(returnView)
				app.updateHighlights(g)
				onSelect("")
				return nil
			}
			if cy == 1 {
				currentDir = filepath.Dir(currentDir)
				drawExplorer(currentDir)
				return nil
			}
			
			idx := cy - 2
			if idx >= 0 && idx < len(files) {
				selected := files[idx]
				newPath := filepath.Join(currentDir, selected.Name())
				if selected.IsDir() {
					currentDir = newPath
					drawExplorer(currentDir)
				} else {
					g.DeleteView("explorer")
					g.SetCurrentView(returnView)
				app.updateHighlights(g)
				onSelect(newPath)
				}
			}
			return nil
		})

		g.SetKeybinding("explorer", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding("explorer", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
		g.SetKeybinding("explorer", 'j', gocui.ModNone, app.cursorDown)
		g.SetKeybinding("explorer", 'k', gocui.ModNone, app.cursorUp)
		g.SetKeybinding("explorer", gocui.MouseWheelDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding("explorer", gocui.MouseWheelUp, gocui.ModNone, app.cursorUp)
	}
}

func (app *App) showInstanceDetails(g *gocui.Gui) {
	maxX, maxY := g.Size()
	v, err := g.SetView("instance_details", 46, 0, maxX-1, maxY-2, 0)
	if err != nil {
		if err.Error() != "unknown view" { return }
		v.Wrap = true
	}
	
	v.Clear()
	
	instView, err := g.View("instances")
	if err != nil { return }
	
	_, cy := instView.Cursor()
	
	if cy < len(app.cfg.Instances) {
		inst := app.cfg.Instances[cy]
		v.Title = fmt.Sprintf(" Instance Details: %s ", inst.Name)
		fmt.Fprintln(v, "\n  [ IDENTITY ]")
		fmt.Fprintf(v, "  Name:          %s\n", inst.Name)
		fmt.Fprintf(v, "  ID:            %s\n", inst.ID)
		fmt.Fprintf(v, "  Type:          %s\n", inst.Type)
		fmt.Fprintln(v, "\n  [ RESOURCES ]")
		fmt.Fprintf(v, "  RAM Limit:     %s\n", inst.Memory)
		fmt.Fprintf(v, "  Container:     %s\n", inst.ContainerName)
		fmt.Fprintln(v, "\n  [ SYSTEM ]")
		fmt.Fprintf(v, "  RCON Port:     %d\n", inst.RCON.Port)
		fmt.Fprintf(v, "  Data Path:     %s\n", inst.Paths.DataDir)
		
		if inst.ID == app.instance.ID {
			fmt.Fprintln(v, "\n  [*] THIS IS YOUR CURRENTLY ACTIVE INSTANCE")
		} else {
			fmt.Fprintln(v, "\n  [ ] Press <Enter> to switch context to this instance.")
		}
	} else if cy == len(app.cfg.Instances)+1 {
		v.Title = " Action: Create Instance "
		fmt.Fprintln(v, "\n  [ CREATE NEW INSTANCE ]")
		fmt.Fprintln(v, "  Launch the interactive wizard to set up a new Minecraft server.")
		fmt.Fprintln(v, "  You will be able to specify the name, RAM limit, and optionally")
		fmt.Fprintln(v, "  import a .mrpack (Modrinth Modpack) to instantly install mods.")
	} else if cy == len(app.cfg.Instances)+2 {
		v.Title = " Action: Delete Instance "
		fmt.Fprintln(v, "\n  [ DELETE CURRENT INSTANCE ]")
		fmt.Fprintf(v, "  Permanently delete the currently active instance: '%s'\n", app.instance.Name)
		fmt.Fprintln(v, "  WARNING: This will destroy the container and erase ALL data!")
	}
}

func (app *App) showInstanceActionPrompt(g *gocui.Gui, inst config.Instance) {
	maxX, maxY := g.Size()
	if v, err := g.SetView("instance_action", maxX/2-30, maxY/2-4, maxX/2+30, maxY/2+5, 0); err != nil {
		if err.Error() != "unknown view" { return }
		v.Title = fmt.Sprintf(" Instance: %s ", inst.Name)
		v.Highlight = true
		v.SelBgColor = gocui.ColorDefault
		v.SelFgColor = gocui.ColorWhite | gocui.AttrBold

		fmt.Fprintln(v, " [!] WARNING: Changing active context.")
		fmt.Fprintf(v, " All server actions will now apply to '%s'.\n", inst.Name)
		fmt.Fprintln(v, " ")
		fmt.Fprintln(v, " [ Switch Context ]")
		fmt.Fprintln(v, " [ Modify RAM ]")
		fmt.Fprintln(v, " [ Cancel ]")

		v.SetCursor(0, 3)
		g.SetCurrentView("instance_action")

		g.SetKeybinding("instance_action", gocui.KeyEsc, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			g.DeleteView("instance_action")
			g.SetCurrentView("instances")
			return nil
		})

		g.SetKeybinding("instance_action", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding("instance_action", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
		g.SetKeybinding("instance_action", 'j', gocui.ModNone, app.cursorDown)
		g.SetKeybinding("instance_action", 'k', gocui.ModNone, app.cursorUp)

		g.SetKeybinding("instance_action", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			_, cy := v.Cursor()
			if cy == 3 { // Switch
				g.DeleteView("instance_action")
				
				app.instance = inst
				if instView, err := g.View("instances"); err == nil {
					app.drawInstances(instView)
				}
				if sv, err := g.View("status"); err == nil {
					app.drawStatus(sv)
				}

				app.updateStatus()
				app.drawTabBar(g)
				app.updateTabContent(g)
				
				if app.logReader != nil {
					app.logReader.Close()
				}
				go app.streamLogsLoop()
				
				g.SetCurrentView("menu")
				app.updateHighlights(g)
				
			} else if cy == 4 { // Modify RAM
				g.DeleteView("instance_action")
				app.showRamSelector(g, "create_form", func(val string) {
					idx := -1
					for i, existing := range app.cfg.Instances {
						if existing.ID == inst.ID {
							idx = i
							break
						}
					}
					if idx != -1 {
						app.cfg.Instances[idx].Memory = val
						config.SaveConfig(app.configPath, app.cfg)
						if app.instance.ID == inst.ID {
							app.instance.Memory = val
						}
						mainView, _ := g.View("main")
						fmt.Fprintf(mainView, "\n[SYSTEM] Applying new RAM configuration: %s...\n", val)
						ctx := context.Background()
						go func() {
							app.dockerAdapter.Stop(ctx, inst.ContainerName)
							app.dockerAdapter.Remove(ctx, inst.ContainerName)
							err := app.dockerAdapter.CreateAndStart(ctx, app.cfg.Instances[idx], func(msg string) {})
							app.gui.Update(func(g *gocui.Gui) error {
								if err != nil {
									fmt.Fprintf(mainView, "[ERROR] %v\n", err)
								} else {
									fmt.Fprintf(mainView, "[SUCCESS] Container recreated with %s RAM.\n", val)
								}
								app.updateStatus()
								return nil
							})
						}()
					}
					g.SetCurrentView("instances")
					app.showInstanceDetails(g)
				})
			} else if cy == 5 { // Cancel
				g.DeleteView("instance_action")
				g.SetCurrentView("instances")
			}
			return nil
		})

		g.SetKeybinding("instance_action", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
		g.SetKeybinding("instance_action", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
		g.SetKeybinding("instance_action", 'j', gocui.ModNone, app.cursorDown)
		g.SetKeybinding("instance_action", 'k', gocui.ModNone, app.cursorUp)
	}
}

func (app *App) drawTabBar(g *gocui.Gui) {
	v, err := g.View("tab_bar")
	if err != nil { return }
	v.Clear()

	tabs := []string{"Console", "Players", "Schedule", "Files", "Config"}
	var display string
	for i, t := range tabs {
		if i > 0 {
			display += " \033[37m-\033[0m "
		} else {
			display += " "
		}

		if i == app.currentTab {
			display += fmt.Sprintf("\033[1;32m%s\033[0m", t) // Bold Green
		} else {
			display += fmt.Sprintf("\033[37m%s\033[0m", t) // Light Gray
		}
	}
	
	// Add the container name at the end
	display += fmt.Sprintf(" \033[37m|\033[0m \033[1;36m%s\033[0m", app.instance.ContainerName)
	
	if app.currentTab == 3 && app.currentFileDir != "" {
		rel := strings.Replace(app.currentFileDir, app.instance.Paths.DataDir, "", 1)
		if rel == "" { rel = "/" }
		display += fmt.Sprintf(" \033[37m-\033[0m \033[1;33m%s\033[0m", rel)
	}
	
	fmt.Fprintf(v, "%s", display)
	
	// Remove the title from the underlying panels so it doesn't overlap weirdly
	viewNames := []string{"main", "tab_players", "tab_schedule", "tab_files", "tab_config"}
	for _, vName := range viewNames {
		if underlying, err := g.View(vName); err == nil {
			underlying.Title = ""
		}
	}
}

func (app *App) updateTabContent(g *gocui.Gui) {
	maxX, maxY := g.Size()
	tabs := []string{"main", "tab_players", "tab_schedule", "tab_files", "tab_config"}
	
	for i, vName := range tabs {
		if i == 0 { continue }
		if v, err := g.SetView(vName, 46, 0, maxX-1, maxY-2, 0); err != nil {
			if err.Error() != "unknown view" { continue }
			v.Frame = true
			v.Wrap = true
			g.SetKeybinding(vName, gocui.KeyArrowRight, gocui.ModNone, app.nextTab)
			g.SetKeybinding(vName, gocui.KeyArrowLeft, gocui.ModNone, app.prevTab)
		}
	}

	g.SetViewOnTop(tabs[app.currentTab])
	g.SetViewOnTop("tab_bar")
	
	if app.currentTab == 1 {
		app.drawPlayersTab(g)
	} else if app.currentTab == 2 {
		app.drawScheduleTab(g)
		app.drawPlayersTab(g)
	} else if app.currentTab == 3 {
		app.drawFilesTab(g)
	} else if app.currentTab == 4 {
		app.drawConfigTab(g)
	}
}

func (app *App) nextTab(g *gocui.Gui, v *gocui.View) error {
	app.currentTab = (app.currentTab + 1) % 5
	app.drawTabBar(g)
	app.updateTabContent(g)
	tabs := []string{"main", "tab_players", "tab_schedule", "tab_files", "tab_config"}
	if g.CurrentView() != nil {
		name := g.CurrentView().Name()
		isRightPanel := false
		for _, t := range tabs {
			if name == t { isRightPanel = true; break }
		}
		if isRightPanel {
			g.SetCurrentView(tabs[app.currentTab])
			app.updateHighlights(g)
		}
	}
	return nil
}

func (app *App) prevTab(g *gocui.Gui, v *gocui.View) error {
	app.currentTab--
	if app.currentTab < 0 {
		app.currentTab = 4
	}
	app.drawTabBar(g)
	app.updateTabContent(g)
	tabs := []string{"main", "tab_players", "tab_schedule", "tab_files", "tab_config"}
	if g.CurrentView() != nil {
		name := g.CurrentView().Name()
		isRightPanel := false
		for _, t := range tabs {
			if name == t { isRightPanel = true; break }
		}
		if isRightPanel {
			g.SetCurrentView(tabs[app.currentTab])
			app.updateHighlights(g)
		}
	}
	return nil
}

func (app *App) drawConfigTab(g *gocui.Gui) {
	v, err := g.View("tab_config")
	if err != nil { return }
	
	
	v.SelBgColor = gocui.ColorDefault
	v.SelFgColor = gocui.ColorWhite | gocui.AttrBold
	
	propsPath := filepath.Join(app.instance.Paths.DataDir, "server.properties")
	props, err := storage.LoadProperties(propsPath)
	if err != nil {
		v.Clear()
		fmt.Fprintf(v, "\n [ERROR] Could not read server.properties: %v\n", err)
		return
	}

	items := []*PropItem{
		{"motd", props.Get("motd", "A Minecraft Server"), false},
		{"online-mode", props.Get("online-mode", "true"), true},
		{"difficulty", props.Get("difficulty", "easy"), false},
		{"max-players", props.Get("max-players", "20"), false},
		{"pvp", props.Get("pvp", "true"), true},
		{"hardcore", props.Get("hardcore", "false"), true},
	}

	drawMenu := func() {
		v.Clear()
		for _, item := range items {
			fmt.Fprintf(v, "[%s] %s\n", item.Key, item.Val)
		}
	}
	drawMenu()
	
	// Bind keys
	g.SetKeybinding("tab_config", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		_, cy := v.Cursor()
		if cy >= len(items) { return nil }
		item := items[cy]

		saveProps := func() {
			for _, i := range items {
				props.Set(i.Key, i.Val)
			}
			props.Save()
			mainView, _ := g.View("main")
			fmt.Fprintf(mainView, "\n[SYSTEM] server.properties updated.\nRestart the server to apply changes.\n")
		}

		if item.IsBool {
			if item.Val == "true" {
				item.Val = "false"
			} else {
				item.Val = "true"
			}
			drawMenu()
			saveProps()
		} else {
			app.showPropInput(g, item, "tab_config", func() {
				drawMenu()
				saveProps()
			})
		}
		return nil
	})

	g.SetKeybinding("tab_config", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
	g.SetKeybinding("tab_config", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
	g.SetKeybinding("tab_config", 'j', gocui.ModNone, app.cursorDown)
	g.SetKeybinding("tab_config", 'k', gocui.ModNone, app.cursorUp)
}

func (app *App) drawFilesTab(g *gocui.Gui) {
	v, err := g.View("tab_files")
	if err != nil { return }
	
	v.SelBgColor = gocui.ColorDefault
	v.SelFgColor = gocui.ColorWhite | gocui.AttrBold
	
	baseDir := app.instance.Paths.DataDir
	if app.currentFileDir == "" || !strings.HasPrefix(app.currentFileDir, baseDir) {
		app.currentFileDir = baseDir
	}

	drawExplorer := func(dir string) {
		v.Clear()
		entries, err := os.ReadDir(dir)
		if err != nil {
			fmt.Fprintf(v, " [ERROR] Could not read directory: %v\n", err)
			return
		}
		
		if dir != baseDir {
			fmt.Fprintln(v, " [DIR] ..")
		}
		
		for _, e := range entries {
			icon := "[FILE]"
			if e.IsDir() { icon = "[DIR]" }
			info, _ := e.Info()
			size := ""
			if info != nil && !e.IsDir() {
				// Convert to KB or MB
				if info.Size() > 1024*1024 {
					size = fmt.Sprintf(" (%.2f MB)", float64(info.Size())/(1024*1024))
				} else if info.Size() > 1024 {
					size = fmt.Sprintf(" (%.2f KB)", float64(info.Size())/1024)
				} else {
					size = fmt.Sprintf(" (%d B)", info.Size())
				}
			}
			fmt.Fprintf(v, " %s %s%s\n", icon, e.Name(), size)
		}
		
		v.SetCursor(0, 0)
		v.SetOrigin(0, 0)
		app.drawTabBar(g) // Update title with new dir
	}
	
	drawExplorer(app.currentFileDir)

	g.SetKeybinding("tab_files", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		_, cy := v.Cursor()
		
		entries, _ := os.ReadDir(app.currentFileDir)
		hasUp := app.currentFileDir != baseDir
		
		if hasUp && cy == 0 {
			app.currentFileDir = filepath.Dir(app.currentFileDir)
			drawExplorer(app.currentFileDir)
			return nil
		}
		
		idx := cy
		if hasUp { idx-- }
		
		if idx >= 0 && idx < len(entries) {
			selected := entries[idx]
			if selected.IsDir() {
				app.currentFileDir = filepath.Join(app.currentFileDir, selected.Name())
				drawExplorer(app.currentFileDir)
			}
		}
		return nil
	})

	g.SetKeybinding("tab_files", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
	g.SetKeybinding("tab_files", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
	g.SetKeybinding("tab_files", 'j', gocui.ModNone, app.cursorDown)
	g.SetKeybinding("tab_files", 'k', gocui.ModNone, app.cursorUp)
}

func (app *App) drawPlayersTab(g *gocui.Gui) {
	v, err := g.View("tab_players")
	if err != nil { return }
	
	v.SelBgColor = gocui.ColorDefault
	v.SelFgColor = gocui.ColorWhite | gocui.AttrBold
	
	v.Clear()
	
	if app.status == "OFFLINE" {
		fmt.Fprintln(v, "\n [!] El servidor está apagado.")
		fmt.Fprintln(v, " Start the server from 'Server Actions' to manage players.")
		return
	}

	password := os.Getenv(app.instance.RCON.PasswordEnv)
	if password == "" {
		password = "secret-dev-password"
	}
	
	client, err := rcon.Dial("127.0.0.1", app.instance.RCON.Port, password)
	if err != nil {
		fmt.Fprintf(v, "\n [ERROR] Could not connect via RCON.\n Error: %v\n", err)
		fmt.Fprintln(v, " Si el servidor apenas está arrancando, espera unos segundos.")
		return
	}
	defer client.Close()

	output, err := client.Execute("list")
	if err != nil {
		fmt.Fprintf(v, "\n [ERROR] Failed 'list' command: %v\n", err)
		return
	}

	fmt.Fprintf(v, "\n  [ Server Report ]\n")
	fmt.Fprintf(v, "  %s\n", strings.TrimSpace(output))
	fmt.Fprintln(v, "  ------------------------------------------------")
	
	// Very simple parse for common format: "There are X of a max of Y players online: Player1, Player2"
	parts := strings.SplitN(output, ":", 2)
	if len(parts) == 2 {
		names := strings.Split(parts[1], ",")
		for _, name := range names {
			cleanName := strings.TrimSpace(name)
			if cleanName != "" {
				fmt.Fprintf(v, "  [PLAYER] %s\n", cleanName)
			}
		}
	} else {
		fmt.Fprintln(v, "  (No players connected or format could not be read)")
	}
}

func (app *App) drawScheduleTab(g *gocui.Gui) {
	v, err := g.View("tab_schedule")
	if err != nil { return }
	
	v.SelBgColor = gocui.ColorDefault
	v.SelFgColor = gocui.ColorWhite | gocui.AttrBold
	
	interval := app.instance.Backup.Interval
	status := "Inactivo"
	if interval > 0 {
		status = fmt.Sprintf("Cada %d hora(s)", interval)
	}

	type option struct {
		Label string
		Value string
	}
	
	opts := []option{
		{"Backup status", status},
		{"Modificar Frecuencia (Horas)", ""},
		{"Disable automatic backups", ""},
		{"Retención máxima", fmt.Sprintf("%d", app.instance.Backup.Keep)},
	}

	drawMenu := func() {
		v.Clear()
		fmt.Fprintln(v, "\n  [ BACKUP SCHEDULE ]")
		fmt.Fprintln(v, "  Configure automatic backups of your world using the Cron system.")
		fmt.Fprintln(v, "  -------------------------------------------------------------------")
		for _, o := range opts {
			val := ""
			if o.Value != "" {
				val = " -> " + o.Value
			}
			fmt.Fprintf(v, "  [*] %s%s\n", o.Label, val)
		}
	}
	drawMenu()

	g.SetKeybinding("tab_schedule", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		_, cy := v.Cursor()
		idx := cy - 4 // Because lines 0, 1, 2, 3 are headers
		
		saveConfig := func() {
			for i, existing := range app.cfg.Instances {
				if existing.ID == app.instance.ID {
					app.cfg.Instances[i] = app.instance
					break
				}
			}
			config.SaveConfig(app.configPath, app.cfg)
			app.drawScheduleTab(g)
			
			exe, _ := os.Executable()
			if app.instance.Backup.Interval > 0 {
				cron.ScheduleBackup(app.instance.ID, exe, app.instance.Backup.Interval)
			} else {
				cron.RemoveBackup(app.instance.ID)
			}
		}

		if idx == 1 { // Modificar frecuencia
			app.showFormInput(g, "Ingresa las horas (ej. 1, 12, 24)", fmt.Sprintf("%d", app.instance.Backup.Interval), "tab_schedule", func(val string) {
				parsed, err := strconv.Atoi(val)
				if err == nil && parsed > 0 {
					app.instance.Backup.Interval = parsed
					saveConfig()
				}
			})
		} else if idx == 2 { // Apagar
			app.instance.Backup.Interval = 0
			saveConfig()
		} else if idx == 3 { // Retencion
			app.showFormInput(g, "Maximum backups to keep (e.g., 5)", fmt.Sprintf("%d", app.instance.Backup.Keep), "tab_schedule", func(val string) {
				parsed, err := strconv.Atoi(val)
				if err == nil && parsed > 0 {
					app.instance.Backup.Keep = parsed
					saveConfig()
				}
			})
		}
		
		return nil
	})

	g.SetKeybinding("tab_schedule", gocui.KeyArrowDown, gocui.ModNone, app.cursorDown)
	g.SetKeybinding("tab_schedule", gocui.KeyArrowUp, gocui.ModNone, app.cursorUp)
	g.SetKeybinding("tab_schedule", 'j', gocui.ModNone, app.cursorDown)
	g.SetKeybinding("tab_schedule", 'k', gocui.ModNone, app.cursorUp)
	
	// Start cursor at first option if it's currently above it
	_, cy := v.Cursor()
	if cy < 4 {
		v.SetCursor(0, 4)
	}
}
