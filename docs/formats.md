# YTool Formats

YTool works with data, often configuration manifests, that represents
some object to some system.  The standard _kind_ of such data follows
JSON: maps, lists, bools, strings, numbers, null.  In this context,
we call such data _object notation_.

YTool supports YAML when it represents object notation, essentially an 
ergonomic layer on top of JSON.  YAML can do much much more than this,
and YTool is not concerned directly with that.

JSON is valid YAML.

YTool introduces a new format, called `Tony`, that differs from YAML.
The differences are intended to preserve the most used ergonomic features of
YAML as possible.

## Working with Kubernetes

YTool works with Kubernetes JSON and YAML.
