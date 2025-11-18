
## Motivation

Kubernetes YAML from sigs.k8s.io/yaml provides a permanent fork of YAML which
interoperates with json by always encoding/decoding it to JSON mid-flight.
This has several benefits, including being able to use json struct tags and
restricting the complexity of programs treating structured data, all the while
allowing comments and in general the ergonomics of YAML.

YTool attempts to fill in gaps with Kubernetes tools for managing and building
manifests.

### YTool vs Helm

Helm uses Go-style templates to generate YAML.  While this allows having a
notion of substitution in YAML, templates are difficult to automatically
generate and maintain as Go-style templates are not YAML based but rather text
based.  This creates an inherent tension when juggling templates and YAML and
templates do not constitute a very readable source for manifests.

Helm's customization via `values.yaml` is built for ease of use only for
`map[string]scalar`, or relatively flat structures.

The results of Helm, i.e. the actual manifests or changes to manifests which
are deployed also can depend on state of the target Kubernetes API.  While this
makes sense w.r.t. applying patches to the Kubernetes API to get to the desired
state, it far from ideal for defining what that desired state is.

By contrast, YTool uses more readable YAML sources, easily nested customisation
values,  and maintains the separation of concerns of a manifest build from the
state of the destination (usually a k8s API server).

### YTool vs Kustomize

Kustomize supports strategic merge patches which provides a successful 
way of using desired pieces of manifests as patches.  While this is
much more readable and maintainable than the alternatives (i.e. JSON Patch
and JSON Merge Patch) it introduces a very fundamental difficulty:  the 
patch and the document to be patched are no longer sufficient to define the
output.

Because Helm uses templates, Kustomize cannot really produce Helm charts.

Kustomize is based on a transformer model, and does not really support
replacements.  However, at the same time, Kustomize places restrictions on what
can be transformed.  For example, one cannot change the YAML kind of a node in
a merge patch (Kind is object/sequence/scalar/document).  This leads to many an
unnecessary restriction; for example, rather than allowing embedding a YAML as
YAML in a node, it has a complicated ConfigMap generator.  

Kustomize does not support substitution, even with its latest attempts to do
so with such mechanisms as its replacements mechanism.  The problem is again
that Kustomize assumes replacements must produce documents with the same
restrictions as strategic merge patches, i.e. not changing the YAML kind
of nodes.

Additionally, the transformer model is difficult to factor into components,
as each component or overlay has a fixed set of inputs, so one cannot
have components which work for multiple sets of inputs, rather they must
be copied.

All this being said, a key benefit of Kustomize is how it achieves readability
by using merge-patch style patches.

In contrast, YTool

- incorporates extended RFC 7396 merge patches, which maintain readability and
  independence of the tool -- your sources define the output without reference to
  anything else, such as upstream "patch strategies", or "server side" patches
- allows for variable substitution of arbitrary yaml associated with variables
- allows for executables you choose to arbitrarily transform or generate data
- is built for flexibility for any kind of sigs.k8s.io/yaml style YAML
- preserves comments and can generate Helm charts and/or YAML comments by means 
  of more expressive beyond-YAML output capabilities

## YTool vs YS/YAML

YTool provides support for scripting via expr-lang.org instead of a clojure 
stype script.  YTool has extensive support for matching and patching documents
in "declarative" merge patch style.

## 

