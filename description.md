# docd: remove virtual clocks

Remove docd's virtual clocks entirely: MountHello.Clock and ClockSpec (docd/api), the clock server and its watches (docd/server/clock.go), the client-session intercept, the mount-session clock hello, .meta/clocks, and the session.md section.

A controller that still sends `clock:` in its hello is refused: MountHello decoding rejects an unknown field.