# Tracksys IIIF Metadata Web Servivce

This is a web service that comminucates directly with the TrackSys DB
to generate IIIF paging metadata for a PID tied to a bibliographic record.

Requires Go 1.6.2+

This package depends on libxml2, so it can't be cross compiled.
To get the build for linux the following must be installed:

* sudo yum install libxml2
* sudo yum install libxml2-devel
