module h3helper

go 1.26

// The frontend source tree contains third-party Go files under node_modules;
// keep them out of ./... package patterns.
ignore (
	./web
)
