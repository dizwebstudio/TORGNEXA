package checker

import (
	"context"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile"
)

func checkProtobuf(ctx context.Context, files []contractFile, problems *diagnostics) {
	if len(files) == 0 {
		return
	}
	sources := make(map[string]string, len(files))
	var names []string
	for _, file := range files {
		name := strings.TrimPrefix(file.Rel, "protobuf/")
		if name == file.Rel {
			problems.add(file.Rel, "protobuf contracts must be under contracts/protobuf")
			continue
		}
		if strings.HasPrefix(name, "google/protobuf/") {
			problems.add(file.Rel, "repository files may not override standard Google protobuf imports")
			continue
		}
		sources[name] = string(file.Data)
		names = append(names, name)
	}
	sort.Strings(names)
	resolver := &protocompile.SourceResolver{Accessor: protocompile.SourceAccessorFromMap(sources)}
	compiler := protocompile.Compiler{
		Resolver:       protocompile.WithStandardImports(resolver),
		MaxParallelism: 1,
	}
	compiled, err := compiler.Compile(ctx, names...)
	if err != nil {
		problems.add("protobuf", "compile: %v", err)
		return
	}
	for _, name := range names {
		file := compiled.FindFileByPath(name)
		if file == nil {
			problems.add("protobuf/"+name, "compiled descriptor is missing")
			continue
		}
		if file.Syntax().String() != "proto3" {
			problems.add("protobuf/"+name, "syntax must be explicitly proto3")
		}
	}
}
