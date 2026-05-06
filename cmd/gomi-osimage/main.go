package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

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
		entries, err := osimagebuild.LoadCatalog(ctx, loadOpts())
		if err != nil {
			return err
		}
		fmt.Printf("valid catalog entries: %d\n", len(entries))
		return nil
	case "matrix":
		fs, loadOpts := catalogFlags("gomi-osimage catalog matrix")
		namesOnly := fs.Bool("names", false, "print one build entry name per line")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		entries, err := osimagebuild.LoadCatalog(ctx, loadOpts())
		if err != nil {
			return err
		}
		matrix := osimagebuild.BuildMatrix(entries)
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
	outDir := fs.String("out-dir", "", "output directory for .raw.zst artifacts")
	workDir := fs.String("work-dir", "", "working directory")
	template := fs.String("template", "", "packer template directory override")
	timeout := fs.String("timeout", "", "packer SSH/build timeout")
	maxSize := fs.String("max-size", "", "maximum artifact size in bytes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	entries, err := osimagebuild.LoadCatalog(ctx, loadOpts())
	if err != nil {
		return err
	}
	size, err := parseOptionalInt64(*maxSize)
	if err != nil {
		return err
	}
	meta, err := osimagebuild.Build(ctx, entries, osimagebuild.BuildOptions{
		EntryName: *name,
		OutDir:    *outDir,
		WorkDir:   *workDir,
		Template:  *template,
		Timeout:   *timeout,
		MaxSize:   size,
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s\n", meta.SHA256, meta.Artifact)
	return nil
}

func runManifest(args []string) error {
	fs := flag.NewFlagSet("gomi-osimage manifest", flag.ContinueOnError)
	dir := fs.String("dir", "", "directory containing per-image metadata and .raw.zst artifacts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return osimagebuild.WriteManifest(*dir)
}

func catalogFlags(name string) (*flag.FlagSet, func() osimagebuild.LoadOptions) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	sourceBase := fs.String("source-base", os.Getenv("GOMI_OS_IMAGE_SOURCE_URL"), "base URL for relative catalog artifact URLs")
	catalogFile := fs.String("catalog", os.Getenv("GOMI_OS_CATALOG_FILE"), "external catalog YAML file")
	catalogURL := fs.String("catalog-url", os.Getenv("GOMI_OS_CATALOG_URL"), "external catalog YAML URL")
	replace := fs.Bool("replace", truthy(os.Getenv("GOMI_OS_CATALOG_REPLACE")), "replace built-in catalog with external catalog")
	return fs, func() osimagebuild.LoadOptions {
		return osimagebuild.LoadOptions{
			SourceBase:      *sourceBase,
			CatalogFile:     *catalogFile,
			CatalogURL:      *catalogURL,
			ReplaceExternal: *replace,
		}
	}
}

func parseOptionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	out, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid --max-size: %w", err)
	}
	return out, nil
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
