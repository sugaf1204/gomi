package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	builder "github.com/sugaf1204/gomi/bootenv/internal/build"
	"github.com/sugaf1204/gomi/bootenv/internal/render"
	"github.com/sugaf1204/gomi/bootenv/internal/spec"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return fmt.Errorf("command is required")
	}
	switch args[0] {
	case "validate":
		fs := flag.NewFlagSet("validate", flag.ContinueOnError)
		fs.SetOutput(stderr)
		file := fs.String("f", "", "BootEnvironment YAML file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		doc, err := load(*file)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "ok: %s\n", doc.Metadata.Name)
		return nil
	case "plan":
		fs := flag.NewFlagSet("plan", flag.ContinueOnError)
		fs.SetOutput(stderr)
		file := fs.String("f", "", "BootEnvironment YAML file")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		doc, err := load(*file)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, render.PlanText(doc))
		return nil
	case "render":
		fs := flag.NewFlagSet("render", flag.ContinueOnError)
		fs.SetOutput(stderr)
		file := fs.String("f", "", "BootEnvironment YAML file")
		output := fs.String("o", "dist/rendered", "output directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		doc, err := load(*file)
		if err != nil {
			return err
		}
		if err := render.Render(doc, *output); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "rendered: %s\n", *output)
		return nil
	case "build":
		fs := flag.NewFlagSet("build", flag.ContinueOnError)
		fs.SetOutput(stderr)
		file := fs.String("f", "", "BootEnvironment YAML file")
		output := fs.String("o", "dist/ubuntu-minimal-cloud-amd64", "artifact output directory")
		cache := fs.String("cache", ".cache", "download cache directory")
		work := fs.String("work", "", "temporary work directory")
		runner := fs.String("runner", defaultRunnerPath(), "deploy runner binary to inject into the rootfs")
		version := fs.String("version", "", "artifact version written to manifest.json")
		keepWork := fs.Bool("keep-work", false, "keep temporary work directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		doc, err := load(*file)
		if err != nil {
			return err
		}
		result, err := builder.Build(context.Background(), doc, builder.Options{
			OutputDir:  *output,
			CacheDir:   *cache,
			WorkDir:    *work,
			RunnerPath: *runner,
			Version:    *version,
			KeepWork:   *keepWork,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "built: %s\n", result.OutputDir)
		return nil
	case "version":
		fmt.Fprintln(stdout, "gomi-bootenv dev")
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func load(path string) (spec.Document, error) {
	if path == "" {
		return spec.Document{}, fmt.Errorf("-f is required")
	}
	return spec.Load(path)
}

func defaultRunnerPath() string {
	if _, err := os.Stat("dist/bin/gomi-deploy-runner"); err == nil {
		return "dist/bin/gomi-deploy-runner"
	}
	return ""
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  gomi-bootenv validate -f bootenv.yaml")
	fmt.Fprintln(w, "  gomi-bootenv plan -f bootenv.yaml")
	fmt.Fprintln(w, "  gomi-bootenv render -f bootenv.yaml -o dist/rendered")
	fmt.Fprintln(w, "  gomi-bootenv build -f bootenv.yaml -o dist/ubuntu-minimal-cloud-amd64")
}
