# DBOT - Demisto Security Bot [![Circle CI](https://circleci.com/gh/demisto/alfred/tree/master.svg?style=svg&circle-token=298d2e89802eaed2e8972abe83baac50d9ee5224)](https://circleci.com/gh/demisto/alfred/tree/master)

A Slack bot to add security info to messages containing URLs, hashes and IPs. You can see it in action at [dbot.demisto.com](https://dbot.demisto.com).

## Authors
This project was built by the [Demisto](https://www.demisto.com) team

## Quick Start

Make sure you have a Go environment set up (either using [GVM](https://github.com/moovweb/gvm/) or just native install)

```sh
$ go get -t -u -d -v github.com/demisto/alfred
```

To get the static part (html, css, js) built install Node.js and then:

```sh
$ cd $GOPATH/src/github.com/demisto/alfred/static/master
$ sudo npm -g install gulp karma bower
$ npm install
$ bower install
$ gulp
```

Please note that there are some files missing from the repository as they contain our sensitive information or our analytics code. To make gulp work, create the following two empty files:
```sh
$GOPATH/src/github.com/demisto/alfred/static/master/jade/_analytics.jade
$GOPATH/src/github.com/demisto/alfred/static/master/jade/_ze.jade
```

Create the Go wrapper around the static files:

```sh
$ go get -v github.com/slavikm/esc
$ cd $GOPATH/src/github.com/demisto/alfred/
$ $GOPATH/bin/esc -o web/static.go -pkg web -prefix static/site/ -ignore \\.DS_Store static/site/
```

And finally, install and run:

```sh
$ cd $GOPATH/src/github.com/demisto/alfred/
$ go install
$ cd $GOPATH/bin
$ ./alfred [-loglevel debug] [-conf path/to/conf] [-logfile path/to/log]
```

If you are running from bin (as above), make sure to create a soft link to the site
```sh
$ ln -s ln -s $GOPATH/src/github.com/demisto/alfred/static/ static
```

Or, you can run directly from the source without installing by:
```sh
$ cd $GOPATH/src/github.com/demisto/alfred/
$ go run alfred.go [-loglevel debug] [-conf path/to/conf] [-logfile path/to/log]
```

Please make sure to run esc again to embed the fully updated site into Go before release.
While developing, you don't need to run esc unless adding new files to the site.

Make sure to specify the Slack client ID and secret in a configuration file. To get VirusTotal reputation, you must specify the VirusTotal key. See conf/conf.go for details.
