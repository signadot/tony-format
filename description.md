# docs: a local mkdocs build leaves docs/hooks/__pycache__/ untracked

`mkdocs build` imports `docs/hooks/article_title.py`, and Python writes `docs/hooks/__pycache__/` beside it. Nothing ignores it, so it shows as untracked after every local build. Add it to `.gitignore`.