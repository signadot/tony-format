# logd remove http server

The logd http server is less than useless from a user perspective,
as maintaining it just adds overhead slowing down our ability to
advance on outstanding issues.

Once we've got parity between new sessions and old http: sessions have
the complete functionality of http handlers, let's axe http at the logd
level.