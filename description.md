# docs: a draft article on applying stored patches to an event stream (the streaming fold)

A second, more technical article beside `docs/articles/logd-document-store.md`, on the apply side only: how `storage/internal/patches` folds stored patches onto a snapshot's events without building the document.

It covers where a patch states something (`Roots`), projection onto a read's path (`api.ProjectDelta`), dominance and `maximalPaths`, the collector and the main loop of `ApplyPatches`, the held key and held comments, grafting patches the base never reaches, and what the fold costs. Producing a diff is out of scope.

The file is a draft, excluded from the site in `mkdocs.yml` as the first article is. Its worked examples were run through `ApplyPatches` before being written down.