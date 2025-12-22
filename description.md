# logd needs schema access

In order to implement !key'd arrays, logd needs to know whether or not
to apply !key based indexing, and currently  this is specified per-patch

obviously, patches may change in this regard over time.

So, logd needs a way to handle this.  I suppose it could track present/absence of !key
tags over time and re-do itself but this seems off.

A natural solution which could also work for schema changes is to have an explicit
SCHEMA method.