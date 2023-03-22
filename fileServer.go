package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var (
	port     = flag.Int("port", 8000, "port to start server on.")
	password = flag.String("password", "", "password to use, if unset no password")
)

var (
	before = `<html>
<head>
  <style>
    table{
      width: 100%;
    }
    table td{
      white-space: nowrap;
    }
    table td:last-child{
      width:100%;
    }
  </style>
</head>
<body>
    <table id="fileTable">
    </table>

    <script type="text/javascript">
        var files = [`
	after = `];

        var sortKey = "name";
        var sortInvert = false;

        // Invert a sort function.
        function invertSort(fn) {
            return function(a, b) { return -fn(a, b); };
        }

        // Standard sort function.
        function cmp(a, b) {
            if (a < b) return -1;
            if (a > b) return 1;
            return 0;
        }

        // Make sizes human readable.
        function humanizeSize(size) {
            if (size > Math.pow(2, 40)) {
                return (size / Math.pow(2, 40)).toFixed(1) + "T";
            }
            if (size > Math.pow(2,30)) {
                return (size / Math.pow(2, 30)).toFixed(1) + "G";
            }
            if (size > Math.pow(2, 20)) {
                return (size / Math.pow(2, 20)).toFixed(1) + "M";
            }
            if (size > Math.pow(2, 10)) {
                return (size / Math.pow(2, 10)).toFixed(1) + "K";
            }
            return "" + size;
        }

        function sortBy(keyType, invert) {
            var fn;
            if (keyType == "name") {
                fn = function(a, b) { return cmp(a.name, b.name); };
            }
            if (keyType == "permissions") {
                fn = function(a, b) { return cmp(a.permissions, b.permissions); };
            }
            if (keyType == "owner") {
                fn = function(a, b) { return cmp(a.owner, b.owner); };
            }
            if (keyType == "group") {
                fn = function(a, b) { return cmp(a.group, b.group); };
            }
            if (keyType == "size") {
                fn = function(a, b) { return cmp(a.size, b.size); };
            }
            if (keyType == "date") {
                fn = function(a, b) { return cmp(a.date, b.date); };
            }
            if (invert) {
                fn = invertSort(fn);
            }
            return fn
        }

        function date2Str(d) {
          return d.getFullYear()  + "-" + (d.getMonth()+1) + "-" + d.getDate() + " " + d.getHours() + ":" + d.getMinutes();
        }

        function filePath(f) {
          if (f.name == "..") return "..";

          path = window.location.pathname;
          if (path == "/") return f;
            
          return path + "/" + f;
        }
    
        function fileToHTMLTR(f) {
            return "<tr><td>" + f.permissions + "</td><td>" 
                + f.owner + "</td><td>"
                + f.group + "</td><td>"
                + humanizeSize(f.size) + "</td><td>"
                + date2Str(f.date) + "</td><td>"
                + "<a href=\"" + filePath(f.encodedName) + "\">" + f.name + "</a>" + "</td></tr>"; 
        }

        function fillTable() {
            files.sort(sortBy(sortKey, sortInvert));

            rootElem = document.getElementById("fileTable");
            header = "<tr>" +
                "<th onclick=\"changeSort('permissions')\">permissions</th>" +
                "<th onclick=\"changeSort('owner')\">owner</th>" +
                "<th onclick=\"changeSort('group')\">group</th>" +
                "<th onclick=\"changeSort('size')\">size</th>" +
                "<th onclick=\"changeSort('date')\">date</th>" +
                "<th onclick=\"changeSort('name')\">name</th>" +
                "</tr>";

            content = "";
            totalSize = 0;
            files.forEach(function(f) {
                content += fileToHTMLTR(f);
                totalSize += f.size;
            })
            content += "<tr><td colspan=\"6\">Total " + humanizeSize(totalSize) + "</td></tr>"
            rootElem.innerHTML = header + content;
        };

        function changeSort(to) {
            if (sortKey == to) sortInvert = !sortInvert;
            sortKey = to;
            fillTable();
        }
        
        fillTable();
    document.title = window.location.pathname;
    </script>
</body>
</html>`
)

type DirLister struct {
	path string
	fs   http.Handler
}

func NewDirLister(path string) *DirLister {
	return &DirLister{path, http.FileServer(http.Dir(path))}
}

func encodeTime(t time.Time) string {
	return fmt.Sprintf("new Date(%d, %d, %d, %d, %d, %d)", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
}

func writeFileDotDotDataJSON(w io.Writer, path string) {
	parentDir := filepath.Clean(path + "/..")
	file, _ := os.Open(parentDir)
	f, _ := file.Stat()

	owner, err := user.LookupId(fmt.Sprintf("%d", f.Sys().(*syscall.Stat_t).Uid))
	if err != nil {
		owner = &user.User{}
	}
	group, err := user.LookupGroupId(fmt.Sprintf("%d", f.Sys().(*syscall.Stat_t).Uid))
	if err != nil {
		group = &user.Group{}
	}

	fmt.Fprintf(w, "{name: \"..\", encodedName: \"..\", permissions: %q, owner: %q, group: %q, size: %d, date: %s},\n",
		f.Mode(), owner.Username, group.Name, f.Size(), encodeTime(f.ModTime()))
}

func writeFileDataJSON(w io.Writer, f os.FileInfo) {
	owner, err := user.LookupId(fmt.Sprintf("%d", f.Sys().(*syscall.Stat_t).Uid))
	if err != nil {
		owner = &user.User{}
	}
	group, err := user.LookupGroupId(fmt.Sprintf("%d", f.Sys().(*syscall.Stat_t).Uid))
	if err != nil {
		group = &user.Group{}
	}

	fmt.Fprintf(w, "{name: %q, encodedName: %q, permissions: %q, owner: %q, group: %q, size: %d, date: %s},\n",
		f.Name(), url.QueryEscape(f.Name()), f.Mode(), owner.Username, group.Name, f.Size(), encodeTime(f.ModTime()))
}

func (d *DirLister) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	uri := req.RequestURI
	if strings.HasPrefix(uri, "/") {
		uri = uri[1:len(uri)]
	}
	if uri == "" {
		uri = "."
	}
	uri, err := url.QueryUnescape(uri)
	if err != nil {
		log.Printf("error unescaping %q: %v", uri, err)
	}
	log.Printf("uri %q requested", uri)

	f, err := os.Open(uri)
	if err != nil {
		log.Printf("error reading directory %q: %v", uri, err)
		w.WriteHeader(500)
		fmt.Fprintf(w, "error reading directory: %v", err)
		return
	}
	stat, err := f.Stat()
	if err != nil {
		log.Printf("error reading directory %q: %v", uri, err)
		w.WriteHeader(500)
		fmt.Fprintf(w, "error reading directory: %v", err)
		return
	}

	if stat.IsDir() {
		files, err := ioutil.ReadDir(uri)
		if err != nil {
			log.Printf("error reading directory %q: %v", uri, err)
			w.WriteHeader(500)
			fmt.Fprintf(w, "error reading directory: %v", err)
			return
		}

		fmt.Fprint(w, before)
		if uri != "." {
			writeFileDotDotDataJSON(w, uri)
		}
		for _, f := range files {
			writeFileDataJSON(w, f)
		}
		fmt.Fprint(w, after)
	} else {
		req.URL.Path = uri
		d.fs.ServeHTTP(w, req)
	}
}

func main() {
	flag.Parse()

	http.Handle("/", NewDirLister("."))
	log.Printf("Starting serving on port %d", *port)

	http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
}
