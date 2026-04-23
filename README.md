Regelmäsig
==========

_Make VBB regelmäsig again, or at least its API._

The [VBB API v6](https://v6.vbb.transport.rest/api.html) service has been quite unreliable.
This upsets things like TRMNL that prefer services it regularly polls to be reliable and 
puts quickly puts them in degraded mode.

Usage
-----

```bash
go install github.com/andrewslotin/regelmaesig
regelmaesig
```
