package main

import (
	"os"

	"terradrift/src/internal/app"
	"terradrift/src/internal/version"
	"terradrift/src/providers/gcp"
)

func main() {
	exit := app.Execute(app.Deps{
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Version:  version.Version,
		Commit:   version.Commit,
		Date:     version.Date,
		Observer: gcp.NewObserver(),
	})
	os.Exit(exit)
}
