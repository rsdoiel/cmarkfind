Installation for development of **cmarkfind**
===========================================

**cmarkfind** is a personal search engine for the Markdown web. It keeps the pages of the
sites you read in one SQLite3 file and searches them locally.

Quick install with curl or irm
------------------------------

There is an experimental installer.sh script that can be run with the following command to install latest table release. This may work for macOS, Linux and if you’re using Windows with the Unix subsystem. This would be run from your shell (e.g. Terminal on macOS).

~~~shell
curl https://rsdoiel.github.io/cmarkfind/installer.sh | sh
~~~

This will install the programs included in cmarkfind in your `$HOME/bin` directory.

If you are running Windows 10 or 11 use the Powershell command below.

~~~ps1
irm https://rsdoiel.github.io/cmarkfind/installer.ps1 | iex
~~~

### If your are running macOS or Windows

You may get security warnings if you are using macOS or Windows. See the notes for the specific operating system you're using to fix issues.

- [INSTALL_NOTES_macOS.md](INSTALL_NOTES_macOS.md)
- [INSTALL_NOTES_Windows.md](INSTALL_NOTES_Windows.md)

Installing from source
----------------------

### Required software


### Steps

1. git clone https://github.com/Laboratory/cmarkfind
2. Change directory into the `cmarkfind` directory
3. Make to build, test and install

~~~shell
git clone https://github.com/Laboratory/cmarkfind
cd cmarkfind
make
make test
make install
~~~

