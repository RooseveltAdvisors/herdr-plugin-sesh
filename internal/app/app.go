package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	clonepkg "github.com/fullerzz/herdr-plugin-sesh/internal/clone"
	"github.com/fullerzz/herdr-plugin-sesh/internal/config"
	connectpkg "github.com/fullerzz/herdr-plugin-sesh/internal/connect"
	"github.com/fullerzz/herdr-plugin-sesh/internal/herdr"
	"github.com/fullerzz/herdr-plugin-sesh/internal/model"
	"github.com/fullerzz/herdr-plugin-sesh/internal/namer"
	pickerpkg "github.com/fullerzz/herdr-plugin-sesh/internal/picker"
	"github.com/fullerzz/herdr-plugin-sesh/internal/preview"
	"github.com/fullerzz/herdr-plugin-sesh/internal/sources"
	"github.com/fullerzz/herdr-plugin-sesh/internal/state"
)

var Version = "dev"

type App struct {
	Out io.Writer
	Err io.Writer
}

func New() *App { return &App{Out: os.Stdout, Err: os.Stderr} }
func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.usage()
	}
	switch args[0] {
	case "--version", "version":
		_, err := fmt.Fprintf(a.Out, "herdr-sesh %s\n", Version)
		return err
	case "list":
		return a.list(ctx, args[1:])
	case "connect":
		return a.connect(ctx, args[1:])
	case "preview":
		return a.preview(ctx, args[1:])
	case "clone":
		return a.clone(ctx, args[1:])
	case "root":
		return a.root(ctx, args[1:])
	case "last":
		return a.last(ctx, args[1:])
	case "last-agent":
		return a.lastAgent(ctx, args[1:])
	case "hook":
		return a.hook(ctx, args[1:])
	case "window":
		return a.window(ctx, args[1:])
	case "plugin":
		return a.plugin(ctx, args[1:])
	case "config":
		return a.config(ctx, args[1:])
	case "picker":
		return a.picker(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func (a *App) usage() error {
	_, err := fmt.Fprintln(a.Out, "herdr-sesh list|connect|preview|clone|root|last|last-agent|hook|window|picker|plugin|config|--version")
	return err
}

func (a *App) warnf(format string, args ...any) {
	if a.Err == nil {
		return
	}
	_, _ = fmt.Fprintf(a.Err, "warning: "+format+"\n", args...)
}

func (a *App) loadConfig(path string) (config.Config, error) {
	cfg, _, err := config.Load(config.LoadOptions{Path: path})
	return cfg, err
}
func (a *App) collect(ctx context.Context, cfg config.Config, target string) ([]model.Session, error) {
	hs := sources.HerdrWorkspaces{Client: herdr.NewCLIClient()}
	srcs := []sources.Source{ignoreSource{hs}, sources.ConfigSessions{Config: cfg}, sources.Zoxide{}}
	if target != "" {
		srcs = append(srcs, sources.DirectPath{
			Path:  target,
			Label: namer.Namer{}.Name(ctx, target, cfg.DirLength),
		})
	}
	merged, err := sources.Merge(ctx, srcs, cfg.SortOrder, cfg.Blacklist, false, true)
	if err != nil {
		return nil, err
	}
	sources.ApplyConfig(&merged, cfg, "")
	return merged.Ordered(), nil
}

type ignoreSource struct{ sources.Source }

func (i ignoreSource) List(ctx context.Context) (model.Sessions, error) {
	ss, err := i.Source.List(ctx)
	if err != nil {
		return model.NewSessions(), nil
	}
	return ss, nil
}

func (a *App) list(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	jsonOut := fs.Bool("json", false, "")
	cfgPath := fs.String("config", "", "")
	blacklisted := fs.Bool("blacklisted", false, "")
	hideDup := fs.Bool("hide-duplicates", true, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, resolvedConfigPath, err := config.Load(config.LoadOptions{Path: *cfgPath})
	if err != nil {
		return err
	}
	cacheable := cfg.Cache && !*blacklisted && *hideDup
	if cacheable {
		if cached, ok, err := state.LoadSessionCache(pluginStateDir(), resolvedConfigPath, 5*time.Second, time.Now()); err != nil {
			a.warnf("ignoring session cache: %v", err)
		} else if ok {
			return a.printSessions(cached, *jsonOut)
		}
	}
	ss, err := sources.Merge(ctx, []sources.Source{ignoreSource{sources.HerdrWorkspaces{Client: herdr.NewCLIClient()}}, sources.ConfigSessions{Config: cfg}, sources.Zoxide{}}, cfg.SortOrder, cfg.Blacklist, *blacklisted, *hideDup)
	if err != nil {
		return err
	}
	sources.ApplyConfig(&ss, cfg, "")
	sessions := ss.Ordered()
	if cacheable {
		if err := state.SaveSessionCache(pluginStateDir(), resolvedConfigPath, sessions, time.Now()); err != nil {
			a.warnf("could not save session cache: %v", err)
		}
	}
	return a.printSessions(sessions, *jsonOut)
}

func (a *App) printSessions(sessions []model.Session, jsonOut bool) error {
	if jsonOut {
		enc := json.NewEncoder(a.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(sessions)
	}
	for _, s := range sessions {
		var err error
		if s.Path != "" {
			_, err = fmt.Fprintf(a.Out, "%s	%s	%s\n", s.Source, s.Name, s.Path)
		} else {
			_, err = fmt.Fprintf(a.Out, "%s	%s\n", s.Source, s.Name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *App) picker(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("picker", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	jsonOut := fs.Bool("json", false, "")
	cfgPath := fs.String("config", "", "")
	fzfPicker := fs.Bool("fzf", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	sessions, err := a.collect(ctx, cfg, "")
	if err != nil {
		return err
	}
	if *jsonOut {
		return a.printSessions(sessions, true)
	}
	pickOpts := pickerpkg.Options{
		Output:                a.Out,
		Prompt:                cfg.TUI.Prompt,
		Placeholder:           cfg.TUI.Placeholder,
		ShowIcons:             cfg.TUI.ShowIcons,
		SeparatorAware:        cfg.SeparatorAware,
		DefaultPreviewCommand: cfg.DefaultSessionConfig.PreviewCommand,
	}
	var selected model.Session
	var ok bool
	if *fzfPicker || strings.EqualFold(os.Getenv("HERDR_SESH_PICKER"), "fzf") {
		selected, ok, err = pickerpkg.RunFZF(ctx, sessions, pickOpts)
	} else {
		client := herdr.NewCLIClient()
		history, historyErr := state.LoadHistory(pluginStateDir())
		if historyErr != nil {
			a.warnf("ignoring workspace history: %v", historyErr)
		}
		pickOpts.RecentWorkspaceIDs = append([]string{currentWorkspaceID()}, history.Workspaces...)
		pickOpts.RecentWorkspaceSort = cfg.TUI.DefaultSort == "recent"
		pickOpts.RefreshAgentStatuses = func() (map[string]string, error) {
			workspaces, err := client.WorkspaceList(ctx)
			if err != nil {
				return nil, err
			}
			statuses := make(map[string]string, len(workspaces))
			for _, workspace := range workspaces {
				statuses[workspace.ID] = workspace.AgentStatus
			}
			return statuses, nil
		}
		selected, ok, err = pickerpkg.Run(sessions, pickOpts)
	}
	if err != nil || !ok {
		return err
	}
	fromWorkspaceID := currentWorkspaceID()
	res, err := connectpkg.Connect(ctx, herdr.NewCLIClient(), []model.Session{selected}, pickerTarget(selected), connectpkg.Options{
		Namer: func(ctx context.Context, p string) string { return namer.Namer{}.Name(ctx, p, cfg.DirLength) },
	})
	if err != nil {
		return err
	}
	a.recordWorkspaceSwitch(fromWorkspaceID, res.Session.WorkspaceID)
	return nil
}

func pickerTarget(s model.Session) string {
	if s.WorkspaceID != "" {
		return s.WorkspaceID
	}
	if s.Path != "" {
		return s.Path
	}
	return s.Name
}

func (a *App) connect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	noFocus := fs.Bool("no-focus", false, "")
	cfgPath := fs.String("config", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("connect requires target")
	}
	target := fs.Arg(0)
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	sessions, err := a.collect(ctx, cfg, target)
	if err != nil {
		return err
	}
	fromWorkspaceID := currentWorkspaceID()
	res, err := connectpkg.Connect(ctx, herdr.NewCLIClient(), sessions, target, connectpkg.Options{NoFocus: *noFocus, Namer: func(ctx context.Context, p string) string { return namer.Namer{}.Name(ctx, p, cfg.DirLength) }})
	if err != nil {
		return err
	}
	if !*noFocus {
		a.recordWorkspaceSwitch(fromWorkspaceID, res.Session.WorkspaceID)
	}
	_, err = fmt.Fprintf(a.Out, "%s\n", res.Session.Name)
	return err
}

func (a *App) preview(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	cfgPath := fs.String("config", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("preview requires target")
	}
	target := fs.Arg(0)
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	sessions, err := a.collect(ctx, cfg, target)
	if err != nil {
		return err
	}
	s, ok := connectpkg.Resolve(sessions, target)
	if !ok {
		s = model.Session{Name: filepath.Base(target), Path: target}
	}
	out, err := preview.Render(ctx, s, cfg.DefaultSessionConfig.PreviewCommand)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(a.Out, out)
	return err
}
func (a *App) clone(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("clone", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	cmdDir := fs.String("cmdDir", "", "")
	dir := fs.String("dir", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("clone requires repo")
	}
	dest, err := clonepkg.Clone(ctx, clonepkg.Request{Repo: fs.Arg(0), CmdDir: *cmdDir, Dir: *dir})
	if err != nil {
		return err
	}
	return a.connect(ctx, []string{dest})
}
func (a *App) root(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("root", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	doConnect := fs.Bool("connect", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := gitRoot(ctx, ".")
	if err != nil {
		return err
	}
	if *doConnect {
		return a.connect(ctx, []string{root})
	}
	_, err = fmt.Fprintln(a.Out, root)
	return err
}
func gitRoot(ctx context.Context, dir string) (string, error) {
	//nolint:gosec // dir is passed as an argv value to a fixed git command.
	b, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
func (a *App) last(ctx context.Context, _ []string) error {
	stateDir := pluginStateDir()
	currentWorkspaceID := currentWorkspaceID()
	id, ok, err := state.WorkspaceToggleTarget(stateDir, currentWorkspaceID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no previous workspace recorded")
	}
	if err := herdr.NewCLIClient().WorkspaceFocus(ctx, id); err != nil {
		if herdr.IsMissingTarget(err) {
			if clearErr := state.ClearWorkspaceLast(stateDir, id); clearErr != nil {
				a.warnf("could not clear unavailable workspace: %v", clearErr)
			}
		}
		return err
	}
	if err := state.ConsumeWorkspaceToggle(stateDir, currentWorkspaceID, id); err != nil {
		a.warnf("could not record workspace history: %v", err)
	}
	return nil
}

func (a *App) lastAgent(ctx context.Context, _ []string) error {
	stateDir := pluginStateDir()
	current := currentAgentRef()
	target, ok, err := state.AgentToggleTarget(stateDir, current)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no previous agent/tab recorded")
	}
	if target.TabID == "" {
		_ = state.ClearAgentLast(stateDir, target)
		return errors.New("previous agent/tab is missing a tab id")
	}
	skipWorkspaceID := ""
	if current.WorkspaceID != "" && target.WorkspaceID != "" && current.WorkspaceID != target.WorkspaceID {
		skipWorkspaceID = target.WorkspaceID
	}
	if err := state.PrepareAgentJump(stateDir, skipWorkspaceID); err != nil {
		a.warnf("could not prepare agent jump: %v", err)
	}
	if err := herdr.NewCLIClient().TabFocus(ctx, target.TabID); err != nil {
		if herdr.IsMissingTarget(err) {
			if clearErr := state.ClearAgentLast(stateDir, target); clearErr != nil {
				a.warnf("could not clear unavailable agent/tab: %v", clearErr)
			}
		}
		if clearSkipErr := state.ClearWorkspaceSkip(stateDir); clearSkipErr != nil {
			a.warnf("could not clear workspace skip: %v", clearSkipErr)
		}
		return err
	}
	if err := state.ConsumeAgentToggle(stateDir, current, target); err != nil {
		a.warnf("could not record agent history: %v", err)
	}
	return nil
}

func currentWorkspaceID() string {
	ctx := pluginContext()
	return firstNonEmpty(os.Getenv("HERDR_WORKSPACE_ID"), ctx.WorkspaceID)
}

func currentAgentRef() state.AgentRef {
	ctx := pluginContext()
	return state.AgentRef{
		WorkspaceID: firstNonEmpty(os.Getenv("HERDR_WORKSPACE_ID"), ctx.WorkspaceID),
		TabID:       firstNonEmpty(os.Getenv("HERDR_TAB_ID"), ctx.TabID),
		PaneID:      firstNonEmpty(os.Getenv("HERDR_PANE_ID"), ctx.FocusedPaneID),
	}
}

func (a *App) hook(_ context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("hook requires event name")
	}
	stateDir := pluginStateDir()
	switch args[0] {
	case "workspace-focused", "workspace.focused":
		// Event payload is authoritative for the newly focused target. Ambient
		// HERDR_* / context can lag behind during rapid focus changes or when a
		// non-default session shares process env with another Herdr instance.
		return state.ObserveWorkspaceFocus(stateDir, firstNonEmpty(eventWorkspaceID(), currentWorkspaceID()))
	case "tab-focused", "tab.focused":
		ctx := pluginContext()
		return state.ObserveAgentFocus(stateDir, state.AgentRef{
			WorkspaceID: firstNonEmpty(eventWorkspaceID(), os.Getenv("HERDR_WORKSPACE_ID"), ctx.WorkspaceID),
			TabID:       firstNonEmpty(eventTabID(), os.Getenv("HERDR_TAB_ID"), ctx.TabID),
			PaneID:      firstNonEmpty(eventPaneID(), os.Getenv("HERDR_PANE_ID"), ctx.FocusedPaneID),
		})
	case "workspace-closed", "workspace.closed":
		return state.ClearWorkspace(stateDir, firstNonEmpty(eventWorkspaceID(), currentWorkspaceID()))
	case "tab-closed", "tab.closed":
		ctx := pluginContext()
		return state.ClearAgent(stateDir, state.AgentRef{
			WorkspaceID: firstNonEmpty(eventWorkspaceID(), os.Getenv("HERDR_WORKSPACE_ID"), ctx.WorkspaceID),
			TabID:       firstNonEmpty(eventTabID(), os.Getenv("HERDR_TAB_ID"), ctx.TabID),
		})
	default:
		return fmt.Errorf("unknown hook event %q", args[0])
	}
}

// pluginStateDir returns the per-Herdr-session state directory.
//
// Herdr gives every session the same HERDR_PLUGIN_STATE_DIR, but workspace and
// tab ids are only unique within a session (both default and a lab session mint
// w1/w2/...). Scoping by HERDR_SESSION keeps last/last-agent pairs orthogonal
// across concurrent sessions and stops lab traffic from poisoning the captain
// default pair.
func pluginStateDir() string {
	base := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if base == "" {
		return ""
	}
	session := strings.TrimSpace(os.Getenv("HERDR_SESSION"))
	if session == "" || session == "default" {
		return base
	}
	return filepath.Join(base, "sessions", hex.EncodeToString([]byte(session)))
}

type pluginInvocationContext struct {
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	FocusedPaneID string `json:"focused_pane_id"`
}

func pluginContext() pluginInvocationContext {
	var ctx pluginInvocationContext
	raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")
	if raw == "" {
		return ctx
	}
	_ = json.Unmarshal([]byte(raw), &ctx)
	return ctx
}

func eventWorkspaceID() string {
	return eventStringField("workspace_id")
}

func eventTabID() string {
	return eventStringField("tab_id")
}

func eventPaneID() string {
	return firstNonEmpty(eventStringField("pane_id"), eventStringField("focused_pane_id"))
}

func eventStringField(key string) string {
	raw := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	if raw == "" {
		return ""
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return ""
	}
	if env.Data == nil {
		return ""
	}
	v, _ := env.Data[key].(string)
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (a *App) recordWorkspaceSwitch(fromWorkspaceID, toWorkspaceID string) {
	if err := state.RecordSwitch(pluginStateDir(), fromWorkspaceID, toWorkspaceID); err != nil {
		a.warnf("could not record workspace history: %v", err)
	}
}

func (a *App) window(ctx context.Context, args []string) error {
	c := herdr.NewCLIClient()
	if len(args) == 0 {
		tabs, err := c.TabList(ctx, currentWorkspaceID())
		if err != nil {
			return err
		}
		for _, t := range tabs {
			if _, err := fmt.Fprintf(a.Out, "%s\t%s\n", t.ID, t.Label); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := c.TabCreate(ctx, herdr.TabCreateRequest{WorkspaceID: currentWorkspaceID(), CWD: args[0], Label: filepath.Base(args[0]), Focus: true})
	return err
}
func (a *App) plugin(ctx context.Context, args []string) error {
	if len(args) >= 1 && args[0] == "open-picker" {
		return herdr.NewCLIClient().PluginPaneOpen(ctx, "RooseveltAdvisors.herdr-sesh", "picker", "overlay")
	}
	return errors.New("unknown plugin command")
}
func (a *App) config(_ context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("config requires path or init")
	}
	dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config", "herdr-sesh")
	}
	switch args[0] {
	case "path":
		_, err := fmt.Fprintln(a.Out, filepath.Join(dir, "sesh.toml"))
		return err
	case "init":
		p, err := config.InitConfig(dir)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(a.Out, p)
		return err
	default:
		return errors.New("unknown config command")
	}
}
