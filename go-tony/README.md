# Tony Format

Tony package houses the reference implementation of the Tony Format
and several related tools.


## O

`o` is a tool for managing objects stored in formats like Tony, YAML, and JSON.

### Overview

```sh
# view files 
o view [ -color ] file [-e path=value ]
o v [ -color ] [-e path=value ] < file
```

```sh
# process a file with embedded transformation instructions
o eval file [-e path=value ] [ files... ]
o e [-e path=value ] < file
```

```bash
# path querys
o list '$...a[*]' [ files... ]
o l '$...a[*]' [ files... ]
o get '$.a[1]' [ files... ]
o g '$.a[1]' [ files... ]
```

```bash
# compute a diff between -- understands tags and strings and arrays!
o diff file1 file2
```

```bash
# match secrets and config maps
kustomize build . | o -y match -s '{ kind: !or [ ConfigMap, Secret] }' -
helm template . | o -y match -s '{ kind: !or [ ConfigMap, Secret] }' -
```

```bash
# patch a file -- understands tags and strings and arrays and diffs,
# all in merge patch style!
o patch -p patch [ file ]
```

```bash
# build manifests and helm charts with matches and patches and embedded transformations
o build [ manifest-build/] [ -l ]  [ -p profile ] [ -e path=value ] [-- path1=value1 path2=value2 ... ]
o b [ manifest-build/] [ -l ]  [ -p profile ] [ -s ] [ -e path=value ]
```
