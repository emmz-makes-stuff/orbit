package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/emmz-makes-stuff/orbit/internal/config"
	"github.com/emmz-makes-stuff/orbit/internal/orb"
	"github.com/emmz-makes-stuff/orbit/internal/workspace"
)

func lifecycleCommands(d *Deps) []*cobra.Command {
	return []*cobra.Command{
		newListCmd(d),
		targetCmd(d, "start [name]", "Start a workspace VM without opening a shell", runStart),
		targetCmd(d, "stop [name]", "Stop a workspace VM, keeping its state", runStop),
		confirmedCmd(d, "destroy [name]", "Delete a workspace VM (files on your Mac are untouched)", runDestroy),
		confirmedCmd(d, "rebuild [name]", "Destroy a workspace VM and create it again from the current config", runRebuild),
		targetCmd(d, "sync [name]", "Re-copy read-only extras from your Mac into a workspace VM", runSync),
	}
}

type targetFunc func(ctx context.Context, s *session, m workspace.Machine) error

// targetCmd is a command acting on the named VM, or on the current directory's workspace.
func targetCmd(d *Deps, use, short string, fn targetFunc) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Long:  short + ".\n\nWith no name, acts on the workspace that covers the current directory.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, m, err := resolveTarget(cmd.Context(), d, args)
			if err != nil {
				return err
			}
			return fn(cmd.Context(), s, m)
		},
	}
}

type confirmedFunc func(ctx context.Context, s *session, m workspace.Machine, yes bool) error

func confirmedCmd(d *Deps, use, short string, fn confirmedFunc) *cobra.Command {
	var yes bool
	cmd := targetCmd(d, use, short, nil)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		s, m, err := resolveTarget(cmd.Context(), d, args)
		if err != nil {
			return err
		}
		return fn(cmd.Context(), s, m, yes)
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return cmd
}

// resolveTarget finds the managed VM a lifecycle command acts on.
func resolveTarget(ctx context.Context, d *Deps, args []string) (*session, workspace.Machine, error) {
	s, err := connect(d)
	if err != nil {
		return nil, workspace.Machine{}, err
	}
	machines, err := s.machines(ctx)
	if err != nil {
		return nil, workspace.Machine{}, err
	}
	if len(args) == 1 {
		name := args[0]
		if !workspace.IsManaged(name) {
			return nil, workspace.Machine{}, fmt.Errorf("%s is not an orbit workspace VM (their names start with %q); see `orbit list`", name, workspace.Prefix)
		}
		for _, m := range machines {
			if m.Name == name {
				return s, m, nil
			}
		}
		return nil, workspace.Machine{}, fmt.Errorf("no workspace VM named %s; see `orbit list`", name)
	}
	dir, err := s.cwd()
	if err != nil {
		return nil, workspace.Machine{}, err
	}
	res := workspace.Resolve(dir, machines)
	if res.Kind != workspace.Found {
		return nil, workspace.Machine{}, fmt.Errorf("no workspace covers %s; name a VM, or see `orbit list`", dir)
	}
	return s, res.Machine, nil
}

func runStart(ctx context.Context, s *session, m workspace.Machine) error {
	if m.State == orb.StateRunning {
		fmt.Fprintf(s.d.Stdout, "%s is already running.\n", m.Name)
		return nil
	}
	if err := s.client.Start(ctx, m.Name); err != nil {
		return fmt.Errorf("start %s: %w", m.Name, err)
	}
	fmt.Fprintf(s.d.Stdout, "Started %s.\n", m.Name)
	return nil
}

func runStop(ctx context.Context, s *session, m workspace.Machine) error {
	if m.State == orb.StateStopped {
		fmt.Fprintf(s.d.Stdout, "%s is already stopped.\n", m.Name)
		return nil
	}
	if err := s.client.Stop(ctx, m.Name); err != nil {
		return fmt.Errorf("stop %s: %w", m.Name, err)
	}
	fmt.Fprintf(s.d.Stdout, "Stopped %s.\n", m.Name)
	return nil
}

func runDestroy(ctx context.Context, s *session, m workspace.Machine, yes bool) error {
	prompt := fmt.Sprintf("Destroy %s (workspace %s)? Files on your Mac are not touched.", m.Name, describeWorkspace(m))
	if err := s.confirm(prompt, yes); err != nil {
		return err
	}
	if err := s.client.Delete(ctx, m.Name); err != nil {
		return fmt.Errorf("destroy %s: %w", m.Name, err)
	}
	fmt.Fprintf(s.d.Stdout, "Destroyed %s.\n", m.Name)
	return nil
}

func runRebuild(ctx context.Context, s *session, m workspace.Machine, yes bool) error {
	if workspace.Orphaned(m, s.d.Stat) {
		return fmt.Errorf("can't rebuild %s: its workspace directory %s is missing; `orbit destroy %s` removes it",
			m.Name, describeWorkspace(m), m.Name)
	}
	// Check the new VM can be built before the old one is gone.
	p, err := s.planFor(m.Workspace)
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf("Rebuild %s (workspace %s)? Anything that exists only inside the VM is lost.", m.Name, m.Workspace)
	if err := s.confirm(prompt, yes); err != nil {
		return err
	}
	if err := s.client.Delete(ctx, m.Name); err != nil {
		return fmt.Errorf("destroy %s: %w", m.Name, err)
	}
	name, err := s.build(ctx, p)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.d.Stdout, "Rebuilt %s.\n", name)
	return nil
}

func runSync(ctx context.Context, s *session, m workspace.Machine) error {
	cfg, err := config.Load(s.d.Home)
	if err != nil {
		return err
	}
	var copies []string
	for _, e := range cfg.ExtrasFor(m.Workspace) {
		if e.Mode != config.ModeRO {
			continue
		}
		if err := s.d.Stat(e.Path); err != nil {
			return fmt.Errorf("extra %s doesn't exist on this Mac; nothing was synced", e.Path)
		}
		copies = append(copies, e.Path)
	}
	if len(copies) == 0 {
		fmt.Fprintf(s.d.Stdout, "%s has no read-only extras to sync.\n", m.Name)
		return nil
	}
	if m.State != orb.StateRunning {
		s.info("starting %s", m.Name)
		if err := s.client.Start(ctx, m.Name); err != nil {
			return fmt.Errorf("start %s: %w", m.Name, err)
		}
	}
	user, err := s.defaultUser(ctx, m.Name)
	if err != nil {
		return err
	}
	if err := copyExtras(ctx, s.client, m.Name, user, copies); err != nil {
		return err
	}
	fmt.Fprintf(s.d.Stdout, "Synced %d read-only extra(s) into %s.\n", len(copies), m.Name)
	return nil
}

// confirm asks a yes/no question on the terminal. Without a terminal it refuses unless yes is set.
func (s *session) confirm(prompt string, yes bool) error {
	if yes {
		return nil
	}
	if !s.d.StdinIsTerminal() {
		return errors.New("refusing without confirmation when stdin isn't a terminal; pass --yes")
	}
	fmt.Fprintf(s.d.Stderr, "%s [y/N] ", prompt)
	answer, _ := bufio.NewReader(s.d.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return errors.New("cancelled")
}

func describeWorkspace(m workspace.Machine) string {
	if m.Workspace == "" {
		return "<no workspace mount>"
	}
	return m.Workspace
}

type listItem struct {
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	State     string `json:"state"`
	Orphaned  bool   `json:"orphaned"`
}

func newListCmd(d *Deps) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workspace VMs, their directories and states",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := connect(d)
			if err != nil {
				return err
			}
			machines, err := s.machines(cmd.Context())
			if err != nil {
				return err
			}
			items := []listItem{}
			for _, m := range machines {
				if !workspace.IsManaged(m.Name) {
					continue
				}
				items = append(items, listItem{
					Name: m.Name, Workspace: m.Workspace, State: m.State,
					Orphaned: workspace.Orphaned(m, d.Stat),
				})
			}
			if asJSON {
				enc := json.NewEncoder(d.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(items)
			}
			if len(items) == 0 {
				fmt.Fprintln(d.Stdout, "No workspace VMs yet. Run `orbit` in a project directory to create one.")
				return nil
			}
			tw := tabwriter.NewWriter(d.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tSTATE\tWORKSPACE")
			for _, it := range items {
				ws := describeWorkspace(workspace.Machine{Workspace: it.Workspace})
				if it.Orphaned {
					ws += "  (orphaned: directory missing)"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", it.Name, it.State, ws)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
