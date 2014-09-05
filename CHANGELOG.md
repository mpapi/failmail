# 0.2.0 (in progress)

## Core functionality

- Changed the way summary emails are sent to recipients. Previously, the
  summary was sent to the union of all recipients in messages that were a part of
  the summary. Now, separate summaries are sent to recipients so that they only
  see the messages that they would normally have been sent.

- Added initial support for AUTH, to require authentication from clients
  connecting to `failmail`.

- Added initial support for STARTTLS, to allow the use of encrypted connections
  to `failmail` from clients.

## For operators

- Added initial support for zero-downtime reloads/upgrades. On receiving
  SIGUSR1, `failmail` shuts itself down and hands its listening socket to a new
  child `failmail` process (which can be from a newer version) and cleanly
  shuts down, letting the new process handle all incoming connections without a
  break in service.

- Added options for config files: `--config` specifies a path to a config
  file (lines of `name = value` pairs), and `--write-config` dumps the current
  set of command line flags into a config file for editing/later use.

- Added handlers for SIGINT and SIGTERM so that the server summarizes and sends
  messages to the upstream server before terminating.

- Added a `--pidfile` flag, to write a pidfile on startup and remove it on
  shutdown.

- Added a `--version` flag, to report the version number and exit.

## For contributors

- Added more automated tests, and did some work toward end-to-end replay
  testing.

- Added the `configure` library for reading config files and creating command
  line flags dynamically from a struct definition, and binding configuration from
  a file/flags to a struct.

- Added to the `parse` library to support config file parsing.


# 0.1.0 (2014-07-20)

Initial release.
