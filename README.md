# cauldron

A Go mutation-testing library that uses schema-based mutation (one compile, many test runs) to outperform existing tools.

## Usage

```
cauldron [--format=text|json] [--timeout=30s] <package-path>
```

## How it works

Cauldron implements the Mutant Schema Generation (MSG) technique: instead of recompiling once per mutant, it encodes all mutants into a single metaprogram, compiles once, and selects active mutants at runtime via the `CAULDRON_MUTANT` environment variable.
