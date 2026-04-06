# wfc-sake-mii-migration

This is a small tool to connect to a
[wfc-server](https://github.com/WiiLink24/wfc-server) instance and sanitize the
personal information from all of the Miis in the databse. Additionally, you can
view and sanitize a given file.

## Usage

This tool has 4 commands as follows

**print**: Takes a Mii file as an argument and prints the relevant sensitive fields

**file**: Takes a Mii file as an argument and sanitizes the given Mii in-place

**sanitize**: Reads `config.yml` and sanitizes every Mii in the specified database

**verify**: Reads `config.yml` and checks every Mii is sanitized in the specified database

## Config

`config.yml` should be configured as follows

```yaml
addr: "127.0.0.1"
port: 5432
username: "wiilink"
password: "password"
databasename: "wwfc"
```
