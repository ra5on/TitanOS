package nvr

import (
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"
)

// node is a small XML tree that ignores namespaces; ONVIF answers differ in
// prefixes from camera to camera.
type node struct {
	Name     string
	Attrs    map[string]string
	Text     string
	Children []*node
}

func parseTree(data []byte) *node {
	dec := xml.NewDecoder(bytes.NewReader(data))
	root := &node{Name: "#root"}
	stack := []*node{root}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{Name: t.Name.Local, Attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.Attrs[a.Name.Local] = a.Value
			}
			top := stack[len(stack)-1]
			top.Children = append(top.Children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			stack[len(stack)-1].Text += string(t)
		}
	}
	return root
}

// find returns all descendants with the given names in order.
func (n *node) find(names ...string) []*node {
	var out []*node
	var walk func(*node)
	walk = func(c *node) {
		for _, ch := range c.Children {
			if ch.Name == names[0] {
				if len(names) == 1 {
					out = append(out, ch)
				} else {
					out = append(out, ch.find(names[1:]...)...)
				}
			}
			walk(ch)
		}
	}
	walk(n)
	return out
}

// child returns the first direct child of that name.
func (n *node) child(name string) *node {
	for _, ch := range n.Children {
		if ch.Name == name {
			return ch
		}
	}
	return nil
}

func (n *node) text() string {
	if n == nil {
		return ""
	}
	return strings.TrimSpace(n.Text)
}

func (n *node) num(name string) int {
	v, _ := strconv.Atoi(n.child(name).text())
	return v
}
