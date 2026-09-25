# Security

tend runs programs, holds terminals and talks to a browser, so a flaw in it
can matter. Thank you for reporting one privately.

## Reporting

Write to **seguranca@auth.com.br** — do not open a public issue. Say what
you found, how to reproduce it, which version (`tend version`) and which
platform. If you prefer, use GitHub's private vulnerability reporting on
this repository instead ("Report a vulnerability" under Security).

What to expect:

- an answer within **3 working days** that the report was received;
- an assessment, and a plan or a question, within **10 working days**;
- a fix released as soon as it is ready, with credit to you in the release
  notes unless you ask otherwise.

Please give us the time to release a fix before telling others.

## Supported versions

Fixes go into the latest release only. `tend update` (or
`tend update -handoff`, which keeps what is running) moves to it.

## What is in scope

- the server and client (`tend`), its automation socket and its handoff;
- the installer (`install.sh`) and the update manifest;
- tend's browser extension and its native messaging bridge;
- the integrations tend installs into agents (`tend integration install`).

Agents themselves (Claude Code, Codex and the others) and the programs run
in panes are their own projects; report their problems to them.

tend is made by Auth Tecnologia Ltda, Belo Horizonte, Brazil.
