# The gomobile bridge calls into these classes from native code by name, so shrinking must
# neither remove nor rename them.
-keep class go.** { *; }
-keep class io.pagecrawl.relay.go.** { *; }
