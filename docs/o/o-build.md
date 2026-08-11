# o build

build is a tool for building manifests.  

Build operates on a build directory, which defaults to the current directory.

Build Object

Build looks for a file called 'build.{tony,objects ,json}' containing a 
build description object in the following form:

  build:
    # env describes the variables that can be set.  It can be any object
    # notation yt understands: tony, objects , json
    # env can be overriden on the command line with '-e path=val' or 
    # '-- key1=val1 key2=val2 ...' or via the environmental variable TONY_DIRBUILD_ENV
    # which may contain a patch for the env, such as '{debug: false}'.
    env:
      debug: true
      object : my-namespace
      # ...
    
    # optional destination directory
    destDir: out

    # sources object what source documents to use 
    sources:
    - dir: source # finds all object files in source relative to current directory.
    - exec: helm template ../../helm/stuf

    # patches are applied to sources
    patchs:
    - if: .[debug]  # condition from env
      match: null  # condition on source document
      patch:
        # ...
      # also can be in a separate file
    - file: my-pathes.tony

Build then 
1. initialises its environment
2. evaluates the sources and patches object descriptions with the environment
3. produces the sources
3. runs the sources through the patches conditionally 
4. takes the results and evaluates them with the environment
5. outputs the result to .destDir or the command output

Environment

Build can have the environment set in 4 ways
1. in the build object file.
2. using '-e path=value'
3. using '-- path1=value1 path2=value2 ...'
4. setting an environment patch in the OS environment variable $TONY_DIRBUILD_ENV

Arguments take precedence over the environment and later arguments take
precedence over earlier ones. Both take precedence over the default environment
specified in the 'env:' field of the build description object.

Profiles

build can have profiles, which are patches to the environment.  To list
profiles associated with the build, run build -l.  To run with a profile, pass
-p <profile> where <profile> is either a name in the list from '-l' or a
filename containing a patch for the environment.  Profiles are expected to be
object files in a sub-directory called 'profiles'.

Show

build -s shows the environment and can be helpful for learning what build
options are available.

Also known as `b`.

## Usage

```
o build [dir] [-l] [-p profile ] [ env ]
```

## Options

### inherited from `o`

| option | type | default | description |
| --- | --- | --- | --- |
| `-b` | bool |  | encode with brackets |
| `-x` | bool |  | expand <<: merge field while encoding |
| `-color` | bool |  | encode with color |
| `-wire` | bool |  | output in compact format |
| `-t`, `-tony` | bool |  | do i/o in tony |
| `-j`, `-json` | bool |  | do i/o in json |
| `-y`, `-yaml` | bool |  | do i/o in yaml |
| `-o` | (filepath) |  | output file (default stdout) |
| `-I`, `-ifmt` | (format) |  | input format: tony/t, json/j, yaml/y |
| `-O`, `-ofmt` | (format) |  | output format: tony/t, json/j, yaml/y |

### `o build` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-l`, `-list` | bool |  | list profiles |
| `-s`, `-show`, `-sh` | bool |  | show environment |
| `-e` | (path=val) |  |  |
| `-p`, `-profile` | profile |  | profile(s) to build (can be specified multiple times) |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/build/)
- [`o`](README.md)
- [`o view`](o-view.md)
- [`o eval`](o-eval.md)
- [`o diff`](o-diff.md)
- [`o get`](o-get.md)
- [`o list`](o-list.md)
- [`o match`](o-match.md)
- [`o patch`](o-patch.md)
- [`o dump`](o-dump.md)
- [`o load`](o-load.md)
- [`o schema`](o-schema.md)
- [`o system`](o-system.md)
- [`o docs`](o-docs.md)

