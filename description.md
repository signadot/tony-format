# automate logd snapshoting with config

logd needs a configuration for snapshotting that will also be extended in the future to include compaction

Other configuration may be included as well -- authentication, limits on supported tags, etc.

The config should be designed for extensibility and the cli system logd serve needs to support it as well.

automatic snapshotting would of course need to be implemented.

no need for a lot of features at this stage, we can start with basic count commits or perhaps size.