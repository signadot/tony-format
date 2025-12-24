# logd: watch hub broadcast timeout too short (100ms) may cause spurious disconnections

DefaultBroadcastTimeout is 100ms which is aggressive. Under load, legitimate clients may be marked as slow consumers and have their watches failed. The buffer is only 100 events, which combined with the short timeout could cause spurious disconnections.

Consider increasing the default timeout or making it configurable.