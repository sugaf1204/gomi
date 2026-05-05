module github.com/sugaf1204/gomi/bootenv

go 1.25.0

require (
	github.com/prometheus/procfs v0.20.1
	github.com/sugaf1204/gomi v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require golang.org/x/sys v0.41.0 // indirect

replace github.com/sugaf1204/gomi => ..
