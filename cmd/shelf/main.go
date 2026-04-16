// Command shelf is the main entry point for the Shelf local-first reading
// journal. Subsequent commits in Session 1 add config loading and package
// wiring; later sessions add the HTTP server, system tray, importer, and
// index.
package main

import "log"

func main() {
	log.Println("shelf: scaffold build — config loading lands in the next commit")
}
