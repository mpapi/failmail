# Failmail

[![Build Status](https://travis-ci.org/hut8labs/failmail.svg?branch=master)](https://travis-ci.org/hut8labs/failmail)

Failmail is an SMTP proxy server that deduplicates and summarizes the emails it
receives. It typically sits between an application and the SMTP server that
application would use to send alerts or exception emails, and prevents the
application from overwhelming its operators with a flood of email.

Its primary goals are:

1. to help the human operators of an application or system under observation to
   understand the errors and notifications that come from the system
2. to prevent an upstream SMTP server from throttling or dropping messages
   under a high volume of errors/alerts
3. to be easier to set up and use than its predecessor,
   [failnozzle](http://github.com/wingu/failnozzle), which had strict
   assumptions about the nature and format of incoming messages

Failmail is language/framework/tool-agnostic, and usually only requires a small
configuration change in your application.


## Installation

To download and build from source:

    $ export GOPATH=...
    $ go get -u github.com/hut8labs/failmail
    $ $GOPATH/bin/failmail

Or, for 64-bit Linux, you can grab a binary of the [latest
release](https://github.com/hut8labs/failmail/releases/latest).


## Usage

See `failmail --help` for a full listing of supported options.

By default, `failmail` listens on the local port 2525
(`--bind="localhost:2525"`), and relays mail to another SMTP server (e.g.
Postfix) running on the local default SMTP port (`--relay="localhost:25"`). It
receives messages and rolls them into summaries based on their subjects,
sending a summary email out 30 seconds (`--wait=30s`) after it stops receiving
messages with those subjects, delaying no more than a total of 5 minutes
(`--max-wait=5m`). Each summary is sent to the union of all of the recipients
of the messages in the summary.

Any summary emails that it can't send via the server on port 25, it writes to a
maildir (`--fail-dir="failed"`; readable by e.g. `mutt`, or any text editor).
If the `--all-dir` option is given, `failmail` will write any email it gets to
a maildir for inspection, debugging, or archival.

### Configuration options

The value of a `failmail` setting is determined as follows:

1. If a config flag is given on the command line for that option, the value of
   that flag is used. Flags may be given with one hyphen or two (`-flag` or
   `--flag`) and with an equals sign before the value or not (`-flag=value` or
   `-flag value`).
2. Otherwise, if a config file is given (via the `--config` flag on the command
   line), and the setting is present in the file, its value from the file is
   used.
3. Finally, the `failmail` default is used. (The values of these defaults are
   shown when running `failmail --help`.)

In general, the command-line flag spells the setting name with hyphens, and the
config file uses underscores. For instance, `--max-wait=5m` is specified in a
config file as `max_wait = 5m`.

The settings are described below:

* `--all-dir` (default: none)

    write all sends to this maildir

* `--batch-expr` (default: `"{{.Header.Get \"X-Failmail-Split\"}}"`)

    an expression used to determine how messages are batched into summary emails

* `--bind-addr` (default: `"localhost:2525"`)

    local bind address

* `--bind-http` (default: `"localhost:8025"`)

    local bind address for the HTTP server

* `--config` (default: none)

    path to a config file

* `--credentials` (default: none)

    username:password for authenticating to failmail

* `--fail-dir` (default: `"failed"`)

    write failed sends to this maildir

* `--from` (default: `"failmail@$(hostname)"`)

    from address

* `--group-expr` (default: `"{{.Header.Get \"Subject\"}}"`)

    an expression used to determine how messages are grouped within summary emails

* `--max-wait` (default: `5m0s`)

    wait at most this long from first message to send summary

* `--pidfile` (default: none)

    write a pidfile to this path

* `--rate-check` (default: `1m0s`)

    how often to check whether rate limit was exceeded

* `--rate-limit` (default: `0`)

    alert if this many emails are received within a window

* `--rate-window` (default: `5`)

    the size of the rate limit window, in check intervals

* `--relay-addr` (default: `"localhost:25"`)

    relay server address

* `--relay-all`

    relay all messages to the upstream server

* `--relay-command` (default: none)

    relay messages by running this command and passing the message to stdin

* `--relay-password` (default: none)

    password for auth to relay server

* `--relay-user` (default: none)

    username for auth to relay server

* `--script` (default: none)

    SMTP session script to run

* `--shutdown-timeout` (default: `5s`)

    wait this long for open connections to finish when shutting down or reloading

* `--socket-fd` (default: `0`)

    file descriptor of socket to listen on

* `--tls-cert` (default: none)

    PEM certificate file for TLS

* `--tls-key` (default: none)

    PEM key file for TLS

* `--version`

    show the version number and exit

* `--wait-period` (default: `30s`)

    wait this long for more batchable messages

* `--write-config` (default: none)

    path to output a config file


## Configuration examples

See the `examples` directory for code snippets for your favorite programming
language/logging framework. (If there's one that you'd like to see, feel free
to open an issue or submit a pull request.)


## Deploying

For now, the best way to run `failmail` in production is via a tool like
`supervisord`, `runit`, or `daemontools`. Here's an example `supervisord`
configuration:

    [program:failmail]
    command=/usr/local/bin/failmail --relay="smtp.mycompany.example.com:25"
    autorestart=true
    autostart=true
    stderr_logfile=/var/log/failmail.err
    stdout_logfile=/var/log/failmail.out


## Development

    $ export GOPATH=...
    $ go get -d github.com/hut8labs/failmail
    $ cd $GOPATH/src/github.com/hut8labs/failmail
    $ ...
    $ go fmt
    $ go build
    $ go test
    $ git commit && git push ...


Other helpful testing tools include the `smtp-source` and `smtp-sink` commands,
which are part of Postfix (and are in the Debian `postfix` package):

    $ smtp-source -v -m 50 127.0.0.1:2525  # send 50 messages to failmail

The Python debugging SMTP server is also useful as an upstream server:

    python -m smtpd -n -c DebuggingServer localhost:3025
