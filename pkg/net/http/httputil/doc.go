package httputil

// krancour: This is a copy of code from the Go language master branch @commit
// 0349f29a55fc194e3d51f748ec9ddceab87a5668. It includes critical bug fixes for
// ReverseProxy that are slated for Go 1.12 and are not available in the GO
// 1.11.5 we're currently using to compile Osiris. When Go 1.12 is released,
// this package can be removed from Osiris.
