# --title Parser fails on YAML files with leading document separator (---)

When using `o build -I y` to parse YAML files that start with `---` (document separator), the parser fails.

**Context:**
controller-gen generates YAML files that start with `---` before the first document. This is valid YAML but causes parse errors in tony.

**Possible solutions:**
1. Use `parse.ParseMulti` in `o build` to handle multi-document YAML
2. Strip leading `---` from input
3. Have controller-gen output JSON instead (using appropriate flags)

**Workaround:**
Strip the leading `---` from generated files before processing.