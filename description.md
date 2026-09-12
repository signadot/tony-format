# compacting log-like data

When logd stores log-like data that grows indefinitely, compaction
doesn't ever make it go away.

the ask in this issue is to design an extension to the compaction
config to allow a user to make logd compaction also delete old
state data that is log-like

perhaps something like

```
- path: a.b.c..
  limit: 1y
- path: a.b.c..
  match:
    status: !or
    - done
    - canceled
  limit: 1d
```

?