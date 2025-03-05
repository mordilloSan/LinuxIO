Linux IO

Copyright @Miguel Mariz - miguelgalizamariz@gmail.com (MIT license)

I am a selftaught programmer that started its homelab journey 5 years ago.

I really liked it and feel in love with docker.

what i didnt like is the ammount of different tools we need to perform system tasks and manage docker

so this is my attempt into creating a simple admin for all common tasks.

uses common linux commands and services

Based on the design by Materio - MUI Next.js Admin Template

this is a next.js app router app with a custom node.js backend

we need to have installed:

FOR SENSORS API: lm-sensors

FOR PAM:

- Centos and RHEL: yum install pam-devel
- Debian/Ubuntu: apt-get install libpam0g-dev
- debian6/maverick/natty: apt-get install libreadline5-dev

FOR NETWORK: lshw

TO DO

1 FOR WIREGUARD

cleanup code for backend.
Implement last handshake on each client
implement download and upload speed
implement is client connected
implement add and remove peers
check conf of clients and server to make sure everythign is ok with the config?

2 FOR PACKAGE UPGRADE

packagekit

3 GENERAL

sudo apt update (and similar for other distro) to refresh the package index. => update API. make this a scheduled event? once in 24h?

detect a system restart needed

Implement dbus-next!!!!! This is big....

Implement websocket!!!!!
