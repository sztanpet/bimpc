Browser-to-browser chat example
===============================

The assets are embedded with //go:embed, so nothing but the Go
toolchain is needed.

To run the example:

	./run

or, to select another port:

    ./run -port=8888

Then open two browser windows to http://localhost:8000/ , change your
name from *J. Doe* to something more distinct, and type messages. See
how they are transmitted to the other browser window.

See the browser's Javascript console for a debug log of incoming and
outgoing messages.
