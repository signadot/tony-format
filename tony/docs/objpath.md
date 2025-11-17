# Querying Objects

Tony provides support for YAMLPath as in [goccy/go-yaml](https://github.com/goccy/go-yaml)

This is rudimentary but works.

```bash
o get -p '$.field[3]'
o list -p '$.field[*]'
```
