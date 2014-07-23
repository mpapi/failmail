# Failmail

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

    $ export GOPATH=...
    $ go get -u github.com/hut8labs/failmail
    $ $GOPATH/bin/failmail


## Usage

See `failmail --help` for a full listing of supported options.

By default, `failmail` listens on the local port 2525
(`--bind="localhost:2525"`), and relays mail to another SMTP server (e.g.
Postfix) running on the local default SMTP port (`--relay="localhost:25"`). It
receives messages and rolls them into summaries based on their subjects,
sending a summary email out 30 seconds (`--wait=30s`) after it stops receiving
messages with those subjects, delaying no more than a total of 5 minutes
(`--wait=5m`). Each summary is sent to the union of all of the recipients of
the messages in the summary.

Any summary emails that it can't send via the server on port 25, it writes to a
maildir (`--fail-dir="failed"`; readable by e.g. `mutt`, or any text editor).
If the `--all-dir` option is given, `failmail` will write any email it gets to
a maildir for inspection, debugging, or archival.


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


## Todo

Work in progress, ideas good and bad, and otherwise:

* exposing Failmail's mechanism for grouping emails/splitting emails among
  summary emails on the command line
* an HTTP or other interface to stats about received mails (e.g. for
  monitoring)
* rate monitoring (a failnozzle feature): send an email to e.g. a pager if the
  number of incoming emails excceeds some limit (there's currently an
  experimental implementation that logs rather than sending email)
* shell script hooks: run a shell command after sending a summary email
* ...
