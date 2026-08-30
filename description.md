# o sys: the admin address does not follow -addr, so a daemon whose data port is free can fail to start

The admin listener's default address is a fixed literal, not derived from the data
address, so moving the data port leaves admin where it was. Combined with a taken admin
port being fatal -- which is deliberate and right -- a second daemon refuses to start
with its own data port free.

Hit on the first attempt at starting a scratch logd beside a running `o sys up`:

    $ o system logd serve -data /tmp/scratch -addr 127.0.0.1:17654
    admin listener: listen tcp 127.0.0.1:9223: bind: address already in use
      (pass -admin-addr <addr> to move it, or -admin-addr off to disable it)

17654 was free. 9223 was held by the live `o sys up` (pid 57432), which had taken it as
ITS default. Passing `-admin-addr 127.0.0.1:17754` starts cleanly and everything works.

## Cause

    cmd/o/logd.go:51            default=localhost:9223
    cmd/o/logd.go:55            AdminAddr: "localhost:9223"
    cmd/o/system_compose.go:27  default=localhost:9223
    cmd/o/system_compose.go:36  AdminAddr: "localhost:9223"
    cmd/o/docd.go:100           default=localhost:9224
    cmd/o/docd.go:104           AdminAddr: "localhost:9224"

6c4b62b describes the default as "lowest data port + 100", and each literal IS that for
its command's default data ports -- logd 9123 -> 9223, docd 9124/9125 -> 9224, `sys up`
9123/9124/9125 -> 9223. That is how the number was chosen, not how it behaves: the
literal does not move when `-addr` does.

## Fix

Derive it from the parsed data address rather than writing the answer down: lowest data
port + 100, computed after flags are parsed, with an explicit `-admin-addr` still
winning. Host should follow too -- an `-addr` on a specific interface with admin pinned
to localhost is a second surprise of the same kind.

Keep the fail-loud behaviour. A quietly absent diagnostic channel is the complaint
s7ftddgrh12krhsfe5n0 fixed, and the error already names both escape hatches; the defect
is only that the default does not follow, so the escape hatch is needed in a case where
nothing was actually in conflict.

## Note

Two daemons started with no flags at all still collide on the admin port, and should --
they collide on the data ports too. This is about the case where the data ports were
deliberately moved and admin did not come along.