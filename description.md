# yaml: reading a manifest changes its values — defaultMode: 0644 comes back the string "0644" where kubectl reads 420

Split out of v0tp11hmh12krncwe5n0 stage 3, where aligning YAML input was planned
before the scope turned out to be larger than numbers.  Checked at 4e41c35.

## A manifest does not survive the round trip

```
$ cat k8s.yaml
volumes:
- configMap:
    defaultMode: 0644

$ o -I yaml -O yaml view k8s.yaml
volumes:
- configMap:
    defaultMode: "0644"
```

Read back with go-yaml, which is what `sigs.k8s.io/yaml` and so kubectl use:

```
k8s.yaml       defaultMode = 420    int
k8s-out.yaml   defaultMode = 0644   string
```

An int became a string, silently, exit 0.  `defaultMode` is an int32 field, so
the output is a manifest the API server will reject — and `0644` is the shape
the Kubernetes docs themselves discuss, since it is the reason they tell you to
write `420`.

This is the audience `docs/motivation.md` is written for, and the tool changes
the manifest as it passes through.

## It is not only that one value

`o -I yaml view` against go-yaml v3, run on the same input:

| written | go-yaml | tony `-I yaml` |
| --- | --- | --- |
| `0644` | int 420 | string `"0644"` |
| `010` | int 8 | string |
| `0x1f`, `0X1F` | int 31 | string |
| `0o777` | int 511 | string |
| `0b1010` | int 10 | string |
| `08` | **float64 8** | string |
| `1_000`, `0_1` | int 1000, int 1 | string |
| `yes`, `no`, `on`, `off` | bool | string |
| `1:30` | int 90, sexagesimal | string |
| `0xzz`, `0o888` | string | string |
| `100m`, `1.2.3` | string | string |

Tony reads none of YAML 1.1's resolution beyond plain decimal.  Strings are all
it agrees on.

## The question this needs answered first

How faithful should the YAML reader be?  It is a real question rather than a
detail, and the table above is why:

- The prefixed forms (`0x`, `0o`, `0b`) are unambiguous.  Every reader agrees,
  YAML 1.2's core schema agrees, and Tony itself now reads them — 4e41c35.
  These are free.
- Leading-zero octal is where the corruption is, and it is genuinely
  contested: `0644` is 420 in YAML 1.1 and 644 under the YAML 1.2 core schema.
  Matching go-yaml means matching YAML 1.1 specifically, and being explicit
  about that.
- `08` resolving to a **float64** is go-yaml being strange rather than a rule
  worth reproducing.  It matches neither the 1.1 int-octal pattern nor
  int-decimal, and falls through to float.  Copying it imports a quirk;
  not copying it means "matches go-yaml" stops being true.
- `yes`/`no`/`on`/`off` and sexagesimals are the [famous
  footguns](https://news.ycombinator.com/item?id=22847940) that `docs/tony.md`
  cites as a reason Tony exists.  Reading them faithfully on *input* is
  arguably right, since the input is YAML and means what YAML means, but it is
  a decision rather than an oversight.

A reasonable answer is "number resolution only": prefixes, leading-zero octal
and underscores, with `08` an error rather than a float, and bools and
sexagesimals left alone.  That fixes the corruption as one coherent unit and
does not pretend to a fidelity it does not have.  But it should be decided
rather than defaulted into.

## Where the code is

- `token/tokenizer.go:601` and `:615` — the digit branch under
  `format.YAMLFormat`.  It scans the plain run, runs `number()` over it, and
  falls back to `yamlPlainToken` when the run is not exactly a decimal number.
  That fallback is where every row of the table becomes a string.
- `token/yaml_plain.go:139` — `yamlPlainToken`, which builds the string.
- `token.RadixLiteral` and `token.RadixNotation` already exist from 4e41c35 and
  cover the prefixed forms, so that part is wiring rather than new logic.

## Rendering the result

Whatever is read has to be written back unambiguously, and the notation tags
from 4e41c35 already do this: a YAML `0644` read as 420 should be written
`0o644`, which is 420 to every reader rather than 420 to one and 644 to
another.  So the output side needs nothing new — `!oct` on the node is enough.