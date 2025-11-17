# Welcome to The Tony Format Docs

The Tony format is a dialect of YAML which strives to provide improved
ergonomics and safety through simplicity and coherent tooling.

Many would place the safety and simplicity of JSON high with very poor
ergonomics.  Likewise, many would rank the ergonomics of YAML for reading and
writing much higher than JSON but then suffer from well known bugs such as those
mentioned [here](https://yamltokyaml.com/en/docs/norway-bug).

Moreover, YAML is often applied to much simpler objects than  YAML the
language is capable of.  For example, in Kubernetes, many tools such as
[kustomize](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/kustomiza
tion/) only output YAML, forcing all who use its data format choice as an
interface to cope with extremely complex parsing and additional risk due to the
underspecification of the language format (eg "it's yaml").

Tony and a few other formats, such as [toml](https://toml.io/en/),
[kyaml](https://medium.com/@simardeep.oberoi/kyaml-kubernetes-answer-to-yaml-s-c
onfiguration-chaos-0c0c09f51587), [json5](https://json5.org) etc try to find a
middle ground.

What makes Tony stand out?  Tony isn't just a variant of json or yaml catered
to a specific need, like CLI config files or ergonomics of reading and writing,
or sanity of parsing. Rather, Tony strives for ergonomic _coherency_ of modeling
and tooling.

## Coherency of Modeling

Tony uses YAML tags as per-node metadata information which makes the meaning
of nodes explicit and compact.  This allows Tony documents to use the same
structure in operations and their inputs.

For example, when querying a set of documents, a match reflects the structure of
the desired matching documents.

```tony
# match the kind field from a set of documents
kind: !or
- ConfigMap
- Secret
```

Another example is diff output where a diff is itself not only a Tony document,
but one which has the exact structure that is shared between the documents
being compared,  as well as the structure of each inserted or deleted part.
(Replacements of course need to be side-by side)

```tony
# document 1
outer:
  inner:
    field-1: 1
    field-2: null
---
# document 2
outer:
  inner:
    field-1: 1
    field-2: 2
---
# diff maintains the structure of document 1 and document 2
# in the same format.
outer:
  inner:
    !replace
    field-2: 2
```

Patches are declarative and apply to any objects modeled by the tony format

```tony
# patch .env list in an input document like it is an object
# whose keys come from the .name field of each element in
# the list.
env: !key(name)
- name: DEBUG
  value: "true"
```

The result is that when working with basic operations like matching/querying,
diffs, and patching both the format and the structure of the related objects are
coherent and uniform.

Other formats mentioned above, including JSON and YAML, lack this property.

## Coherency of Tooling

Tony addresses tooling ergonomics in part with the modeling coherencies
mentioned above, and in part with tooling.

### What is Coherency of Tooling

We have taken the stance that coherency of tooling means

- _Composability_ with industry standard tooling for object notation
- _Internal Composability_  between operations.
- A _complete_ support surface for all aspects of the format (eg comments, tags, translation)
- _Removing and minimizing_ unnecessary steps for basic tasks

While it would be unreasonable to claim that these goals have been achieved,
Tony format's tooling takes these goals seriously and the results are satisfying.

### Highlights

Some highlights of the tooling level coherency are

- Smart normalized formatting when encoding to tony.
- LSP support.
- Diffs _are_ merge patches.
- Easily parse and produce YAML representing payloads and configuration objects.
- Ease of switching to and from bracketed notation.
- Ease of producing JSON for relevant objects.
- Built-in support for interfacing with text templating.
- Tooling level support for comments.

