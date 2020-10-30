# run application

# run from here, since application expects json template in templates/
cd bin; ./iiifsrv -tracksys $TRACKSYS_URL -apollo $APOLLO_URL -host $IIIF_MANIFEST_HOST -iiif $IIIF_URL -bucket $IIIF_CACHE_BUCKET -rooturl $IIIF_CACHE_ROOT_URL

#
# end of file
#
