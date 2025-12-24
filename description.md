# logd: forwardEvents swallows replay errors - client unaware of incomplete replay

Errors during watch replay in forwardEvents are logged but the watch continues:

if err \!= nil {
    s.log.Error("failed to read state for replay", ...)
} else {

The client receives no indication that replay was incomplete. Consider sending an error event or failing the watch entirely.