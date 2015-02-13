#!/bin/bash

printf 'package main\n\nconst VERSION = "%s-%s"\n' $(date +%Y%m%d) $(git rev-parse --short HEAD) > version.go
