# docs: a draft article on logd's storage engine for dev.to/substack

Draft at docs/articles/logd-document-store.md, carrying dev.to front matter plus status: draft.

Covers the indexed event representation, streaming diff application, the A/B log files backing snapshots, compaction, retention, and copy-on-write scopes -- then compares the design against etcd and against JSON in a relational column.

The comparison opens by naming that it is uneven: etcd and Postgres are whole systems, logd is a storage engine with a session protocol on it, which is closer to putting MyISAM next to Postgres than to a like-for-like. A docd article is the natural follow-up and would carry routing, composition and mounts.

Open before publishing:

- the article is excluded from the site in mkdocs.yml while it is a draft; publishing means dropping that line and adding a nav entry, or not shipping it to the site at all if it lives only on dev.to
- it names two issues by id (qqq1jejg leases, nkn7ptxc HA) using 'git issue show', which reads oddly to anyone outside the repo -- worth resolving before it goes out
- length is ~3000 words; a 2750 target was wanted and the last cut made was the secondary-index worked example