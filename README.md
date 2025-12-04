# Paprika 🌶️

Unofficial Go client library and CLI utility for the Paprika recipe manager.

Based on the work of [paprika-api](https://github.com/joshstrange/paprika-api) 
and [go-paprika](https://github.com/willgorman/go-paprika).

## Features

### CLI Utility

The CLI supports one-way (cloud -> local) synchronization of Paprika recipes and categories.
The core purpose (for now) and original motivation for this tool is to enable periodic,
incremental backups of data so that folks (like me) who rely on Paprika to manage their 
recipe collection are able to rest easy knowing that they are better-protected from
data loss.

By default, recipes deleted from Paprika will continue to exist locally.
If this is undesirable, sync operations can be configured to purge deleted recipes
from the local system. Recipes can be purged on a subsequent sync according to a 
"not-seen-since" interval (recommended), or immediately.

### Client Library

A client libary is also provided for interacting with the Paprika API.
Currently, the client library supports read-only fetch operations and cannot be used
to create, modify, or delete Paprika recipes or other resources.
