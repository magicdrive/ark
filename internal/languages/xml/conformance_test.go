package xml

import (
	"testing"

	"github.com/magicdrive/ark/internal/conformance"
)

// The fixtures proposed for internal/conformance/conformance_test.go,
// checked here against the provider contract.
var (
	conformanceValid = []conformance.Case{
		{Name: "pom", File: "pom.xml", Source: []byte("<?xml version=\"1.0\"?>\n<project xmlns=\"http://maven.apache.org/POM/4.0.0\">\n  <modelVersion>4.0.0</modelVersion>\n  <parent>\n    <groupId>com.example</groupId>\n    <artifactId>parent</artifactId>\n    <version>1.0</version>\n  </parent>\n  <artifactId>app</artifactId>\n  <modules>\n    <module>core</module>\n  </modules>\n  <dependencies>\n    <dependency>\n      <groupId>org.slf4j</groupId>\n      <artifactId>slf4j-api</artifactId>\n      <version>2.0.13</version>\n    </dependency>\n  </dependencies>\n</project>\n")},
		{Name: "namespaces", File: "docs/book.xml", Source: []byte("<?xml version=\"1.0\"?>\n<b:book xmlns:b=\"urn:book\" xmlns:xi=\"http://www.w3.org/2001/XInclude\" xmlns:xlink=\"http://www.w3.org/1999/xlink\">\n  <b:title xml:id=\"top\">Title</b:title>\n  <b:para>See <b:link xlink:href=\"#top\">the title</b:link>.</b:para>\n  <xi:include href=\"chapter.xml\"/>\n</b:book>\n")},
		{Name: "data", File: "config/app.xml", Source: []byte("<config>\r\n  <!-- settings -->\r\n  <server host=\"localhost\" port=\"8080\"/>\r\n  <server host=\"example.com\"/>\r\n  <script><![CDATA[if (a < b) {}]]></script>\r\n</config>\r\n")},
	}
	// An element closed by the wrong end tag: the elements read stay.
	conformanceBroken = conformance.Case{Name: "broken", File: "broken.xml", Source: []byte("<a>\n  <b>\n</a>\n<c/>\n")}
	// A dependency whose groupId is a property the file does not define.
	conformanceDynamic = conformance.Case{Name: "dynamic", File: "dyn/pom.xml", Source: []byte("<project><artifactId>a</artifactId><dependencies><dependency><groupId>${vendor}</groupId><artifactId>lib</artifactId></dependency></dependencies></project>\n")}
)

func TestConformance(t *testing.T) {
	conformance.RunContract(t, NewProvider(), conformanceValid)
	conformance.CheckBrokenSourceDiagnosed(t, NewProvider(), conformanceBroken)
	conformance.RunContract(t, NewProvider(), []conformance.Case{conformanceDynamic})
}
