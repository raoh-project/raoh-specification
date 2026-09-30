# Paths

An issue's path says where in the input the problem is. A path is a sequence of segments; a segment
is an object member's name or an array element's index. The input itself has the empty path.

A path is written as a JSON Pointer (RFC 6901): each segment preceded by `/`, with `~` in a segment
written `~0` and `/` written `~1`, and an index written in decimal. The empty path is written as the
empty string. The member `a/b` is at `/a~1b`, the member `~c` at `/~0c`, and the third element of
the member `items` at `/items/2`.

A path is kept as its segments and written only when it is reported. An implementation that kept the
written form and appended to it would escape a segment twice or not at all.

The way an implementation shows a path to a person, in an error's `toString` for instance, is not
specified.
