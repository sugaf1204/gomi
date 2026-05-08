package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimagebuild"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "catalog":
		return runCatalog(ctx, args[1:])
	case "build":
		return runBuild(ctx, args[1:])
	case "manifest":
		return runManifest(args[1:])
	default:
		return usage()
	}
}

func runCatalog(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return catalogUsage()
	}
	switch args[0] {
	case "validate":
		fs, loadOpts := catalogFlags("gomi-osimage catalog validate")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		entries, err := oscatalog.Load(ctx, loadOpts())
		if err != nil {
			return err
		}
		fmt.Printf("valid catalog entries: %d\n", len(entries))
		return nil
	case "matrix":
		fs, loadOpts := catalogFlags("gomi-osimage catalog matrix")
		buildConfig := fs.String("build-config", "", "OS image build config YAML path")
		namesOnly := fs.Bool("names", false, "print one build entry name per line")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		entries, err := oscatalog.Load(ctx, loadOpts())
		if err != nil {
			return err
		}
		cfg, err := osimagebuild.LoadConfig(*buildConfig)
		if err != nil {
			return err
		}
		matrix, err := osimagebuild.BuildMatrix(entries, cfg)
		if err != nil {
			return err
		}
		if *namesOnly {
			for _, entry := range matrix.Include {
				fmt.Println(entry.Name)
			}
			return nil
		}
		raw, err := json.Marshal(matrix)
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	default:
		return catalogUsage()
	}
}

func runBuild(ctx context.Context, args []string) error {
	fs, loadOpts := catalogFlags("gomi-osimage build")
	name := fs.String("name", "", "catalog entry name to build")
	buildConfig := fs.String("build-config", "", "OS image build config YAML path")
	outDir := fs.String("out-dir", "", "output directory for rootfs SquashFS artifacts")
	workDir := fs.String("work-dir", "", "working directory")
	processors := fs.Int("processors", 1, "mksquashfs processor count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	entries, err := oscatalog.Load(ctx, loadOpts())
	if err != nil {
		return err
	}
	cfg, err := osimagebuild.LoadConfig(*buildConfig)
	if err != nil {
		return err
	}
	meta, err := osimagebuild.Build(ctx, entries, cfg, osimagebuild.BuildOptions{
		EntryName:  *name,
		OutDir:     *outDir,
		WorkDir:    *workDir,
		Processors: *processors,
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s\n", meta.SHA256, meta.Artifact)
	return nil
}

func runManifest(args []string) error {
	fs := flag.NewFlagSet("gomi-osimage manifest", flag.ContinueOnError)
	dir := fs.String("dir", "", "directory containing per-image metadata and rootfs SquashFS artifacts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return osimagebuild.WriteManifest(*dir)
}

func catalogFlags(name string) (*flag.FlagSet, func() oscatalog.LoadOptions) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	sourceBase := fs.String("source-base", os.Getenv("GOMI_OS_IMAGE_SOURCE_URL"), "base URL for relative catalog artifact URLs")
	catalogFile := fs.String("catalog", os.Getenv("GOMI_OS_CATALOG_FILE"), "external catalog YAML file")
	catalogURL := fs.String("catalog-url", os.Getenv("GOMI_OS_CATALOG_URL"), "external catalog YAML URL")
	replace := fs.Bool("replace", truthy(os.Getenv("GOMI_OS_CATALOG_REPLACE")), "replace built-in catalog with external catalog")
	return fs, func() oscatalog.LoadOptions {
		return oscatalog.LoadOptions{
			SourceBase:      *sourceBase,
			CatalogFile:     *catalogFile,
			CatalogURL:      *catalogURL,
			ReplaceExternal: *replace,
		}
	}
}

func usage() error {
	return fmt.Errorf("usage: gomi-osimage catalog {validate|matrix} | gomi-osimage build --name NAME | gomi-osimage manifest")
}

func catalogUsage() error {
	return fmt.Errorf("usage: gomi-osimage catalog {validate|matrix}")
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
