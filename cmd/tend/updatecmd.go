package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/update"
)

// `tend update` and `tend channel`, herdr's two update commands.
//
// Nothing here runs on its own. herdr checks for updates in the background
// and offers one; tend asks nothing and downloads nothing until somebody runs
// the command, because a program that replaces its own binary unprompted is a
// program you have to trust more than this one has earned.

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "say what is published and stop")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend update [-check]\n\n"+
				"downloads the build published on the configured channel and puts it in\n"+
				"place of this one. the download is checked against the manifest's\n"+
				"checksum before anything is installed.\n\n"+
				"set the channel's manifest URL in the settings file first:\n\n"+
				"  [update]\n"+
				"  channel = \"stable\"\n"+
				"  manifest = \"https://…/latest.json\"\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	url := cfg.ManifestURL()
	if url == "" {
		return fmt.Errorf("no manifest for the %q channel; set [update] manifest in the settings file",
			channelName(cfg))
	}

	manifest, err := update.Fetch(url)
	if err != nil {
		return err
	}
	release, err := update.ReleaseFor(manifest)
	if err != nil {
		return err
	}

	if !update.Differs(release, version) {
		fmt.Fprintf(os.Stderr, "%s already on %s (%s)\n", tag(), release.Version, channelName(cfg))
		return nil
	}
	fmt.Fprintf(os.Stderr, "%s %s publishes %s; this is %s\n", tag(), channelName(cfg), release.Version, version)
	if release.Notes != "" {
		fmt.Println(release.Notes)
	}
	if *check {
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	downloaded, err := update.Download(release, self)
	if err != nil {
		if errors.Is(err, update.ErrChecksum) {
			// Worth saying plainly: it is the one failure that means the
			// bytes served were not the bytes published.
			return fmt.Errorf("%w — nothing was installed", err)
		}
		return err
	}
	if err := update.Install(downloaded, self); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s installed %s at %s\n", tag(), release.Version, self)
	fmt.Fprintf(os.Stderr, "%s run \"tend handoff\" to move running sessions onto it\n", tag())
	return nil
}

// runChannel shows or sets which channel updates come from.
func runChannel(args []string) error {
	fs := flag.NewFlagSet("channel", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend channel [stable|preview]\n\n"+
				"with no argument, says which channel this tend follows.\n\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if fs.NArg() == 0 {
		url := cfg.ManifestURL()
		if url == "" {
			url = "(no manifest configured)"
		}
		fmt.Fprintf(os.Stderr, "%s channel %s · %s\n", tag(), channelName(cfg), url)
		return nil
	}

	channel := update.Channel(fs.Arg(0))
	if !channel.Valid() {
		return fmt.Errorf("%q is not a channel; use stable or preview", fs.Arg(0))
	}
	if err := config.Set("update", "channel", config.Quote(string(channel))); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s channel %s\n", tag(), channel)
	return nil
}

func channelName(cfg config.Config) string {
	if cfg.Update.Channel == "preview" {
		return "preview"
	}
	return "stable"
}
