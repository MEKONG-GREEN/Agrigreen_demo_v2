package domain

import "slices"

// CopySources prevents application registration from mutating shared defaults.
func CopySources(in map[string]SourceSpec) map[string]SourceSpec {
	out := make(map[string]SourceSpec, len(in))
	for key, spec := range in {
		spec.Levels = slices.Clone(spec.Levels)
		spec.Columns = slices.Clone(spec.Columns)
		out[key] = spec
	}
	return out
}
func NativeSources() map[string]SourceSpec { return CopySources(nativeSourceSpecs) }
func NativeBuiltins() []Template           { return Builtins() }
