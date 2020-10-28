package main

// CacheProxy contains methods for accessing the cache
type CacheProxy struct {
	config *serviceConfig
}

// InitializeService will initialize the service context based on the config parameters.
func NewCacheProxy(cfg *serviceConfig) *CacheProxy {

	proxy := CacheProxy{
		config: cfg,
	}
	return &proxy
}

// IsInCache identifies if the specified key is in the cache
func (svc *CacheProxy) IsInCache( key string ) bool {
	return false
}

// ReadFromCache reads the contents of the specified cache element
func (svc *CacheProxy) ReadFromCache( key string ) ( string, error ) {
	return "cache", nil
}

// WriteToCache writes the contents of the specified cache element
func (svc *CacheProxy) WriteToCache( key string, content string ) error {
	return nil
}

//
// end of file
//