# XML and Maven POM support: design record and evaluation

This page records why XML support is built the way it is, what was
measured, and how. The rules themselves are in the headers of
`internal/languages/xml/provider.go`,
`internal/languages/xml/xmldoc/xmldoc.go` and
`internal/languages/xml/maven/maven.go`.

## Parser

- **Library.** The standard library's `encoding/xml` (Go 1.27.3), strict,
  token by token (`Decoder.RawToken`). No new dependency. It checks the
  lexical syntax — names, quoting, characters and UTF-8, comments, CDATA,
  processing instructions, the brackets of a DOCTYPE — and reports the byte
  offset after each token (`InputOffset`); tokens are contiguous, so a
  token spans from the previous offset to its own.
- **What the library leaves to its caller, and Ark adds** (`xmldoc`):
  matching end tags (`RawToken` does not check them; Ark reports a
  mismatch and recovers at the nearest open element of the name), namespace
  prefixes (an undeclared prefix is a diagnostic and a pseudo-namespace of
  its own), duplicate attributes (by namespace URI and local name — the
  library accepts them), one root element and no text outside it, the
  bytes of each attribute (a scan of the start tag: the library gives the
  tag's bytes only) and where an element ends.
- **Not chosen.** No Tree-sitter XML grammar is bundled, and a hand-written
  parser would repeat what the standard library already checks.

## Security

The design goal is that reading an XML file costs what reading its bytes
costs, and reads nothing else.

- **No external access.** `encoding/xml` never opens a DTD, an external
  entity or a URL, and Ark never sets a `CharsetReader` (a document in an
  encoding other than UTF-8 or ASCII, by its byte order mark or its XML
  declaration, is `xml_unsupported_encoding` and not read). A DOCTYPE that
  names a `SYSTEM` or `PUBLIC` identifier — an external DTD or an external
  entity — is `xml_external_entity`: reported, never read.
- **No entity expansion.** `encoding/xml` knows no DTD, so an entity a
  DOCTYPE declares is unknown to it, and in strict mode an unknown entity is
  an error. In a document with a DOCTYPE, Ark gives every entity reference
  the source spells (`&name;`, at most 10000 distinct names) its own
  spelling as its replacement text; the decoder inserts the replacement
  once and never scans it again. So a billion-laughs document is as long
  read as written, and `&xxe;` of `<!ENTITY xxe SYSTEM "file:///etc/passwd">`
  stays the text `&xxe;` (`xml_entity_not_expanded`, once per file).
  Without a DOCTYPE an undeclared entity is a well-formedness error, as XML
  defines it (`xml_syntax`). Predefined entities and character references
  are replaced, as XML requires.
- **Bounds.** A file over 4 MiB is not read (`xml_too_large`); nesting
  deeper than 256 (`xml_too_deep`) and more than 200000 elements and
  attributes (`xml_too_many_nodes`) end the reading, keeping what was read;
  an element keeps at most 1 KiB of its own text; at most 1000 diagnostics.
  Every bound is a diagnostic, never a panic.
- **Names are data.** Text, CDATA, comments and processing instructions
  declare nothing and never enter a name or a signature. Every name,
  identity and reference name taken from the file is a one-line name
  (`declname.IsName`); the file's own path, which the index chose, is
  exempt within an identity (`<FileID>#…`), so a file of a long path still
  declares its elements. A namespace URI is a signature only when it is a
  name. Entities left as written reach a name only as their own spelling
  (`&g;`), never their replacement.
- **Tests.** `TestSecurity` (billion laughs, external SYSTEM entities and a
  parameter entity naming a URL, an external DTD, an external entity in a
  POM's coordinates) and `TestSecurity_Hostile` (100000 open tags, a 3 MiB
  attribute, 100000 entity references in an attribute, NUL, an overlong
  UTF-8 sequence, 50000 entity declarations), with `TestBounds` and
  `TestDiagnostics`.

## Model

- **Two layers.** `xmldoc` knows XML; `maven` knows the POM and reads only
  `xmldoc`'s elements. A file is a POM when its root is `<project>` in the
  POM namespace (4.0.0 or 4.1.0), or in no namespace in a file named
  `pom.xml`.
- **Element identities** are XPath-like paths from the root,
  `<file>#/project/dependencies/dependency[2]` (the first sibling of a name
  has no `[1]`). Elements are the same by namespace URI and local name, not
  by the prefix written. A namespace is labelled by the first prefix the
  document binds it to, "" when only a default declaration binds it; two
  namespaces that would share a label are `label~2`, `label~3`, … in
  document order (`~` is no character of an XML name, so no label collides
  with a real prefix). A POM's elements, in the POM namespace by default,
  read `/project/artifactId`.
  - *Stable:* inserting a sibling of another name, an attribute, a comment
    or text keeps every identity (`TestIdentityStability`).
  - *Limits:* inserting a same-named sibling before others renumbers those
    after it; a new namespace declaration earlier in the document may
    relabel a namespace; a path over 256 bytes is no name, and the element
    and everything under it are not declared.
- **Attributes** are `…/@name` (`…/@label:name` in a namespace; an
  unprefixed attribute is in no namespace whatever the elements' default);
  namespace declarations are namespaces `…/@xmlns`, `…/@xmlns:p`, their URI
  the signature.
- **Bounds and priority.** At most 5000 declarations and 20000 references a
  file: the document and the POM's declarations first, then IDs, then
  elements breadth first — shallow before deep — up to 3000, then the
  attributes of the declared elements in the same order; past them
  `xml_truncated`.
- **Relations.** `contains` from the document to its root, from an element
  to its child elements and attributes (the reference's name is the child's
  identity).
- **IDs.** `xml:id` is XML's own ID attribute: an anchor `<file>#<id>`,
  named `#<id>`. Without a schema or DTD, which Ark does not read, no other
  attribute is an ID by its name — but `id` in no namespace is one in most
  vocabularies (XHTML, SVG, DocBook, Spring), so it is an anchor too,
  capped at Candidate (`SymbolDraft.ConfidenceCap`): no reference becomes an
  edge on that convention alone. A fragment `href="#x"` is an
  `id_reference`: XLink's `xlink:href` (a URI reference by definition)
  Exact, an `href` in no namespace at most Strong. An ID declared twice is
  two anchors of one identity: Candidates. Other IDREF / IDREFS attributes
  (`ref`, `idref`, `linkend`, …) are no references: only a schema says
  which attributes are IDREFs.
- **Relative references.** An XInclude `<xi:include href>` and an
  `<?xml-stylesheet href?>` whose href is a plain relative path (no scheme,
  drive, query, fragment, `%` escape, backslash or property; no `xml:base`
  around it) is a `resource_link` with path evidence (`FileIdentity`) to the
  file's declaration, of any language; one that leaves the repository is
  outside it. Schema locations are not read (mostly URLs, and a local
  `.xsd` is no file Ark indexes).
- **Identity only.** Every XML file is `IdentityOnly`: no Java (or other)
  name reaches an XML element, and no XML reference resolves by a name
  match (`TestGraph_JavaUnaffected`).

## Maven

- **Project** (`package`): `<file>::<groupId>:<artifactId>`, named
  `groupId:artifactId`, the version and packaging in the signature. A
  groupId or version the file does not write is its `<parent>`'s, as Maven
  inherits them; a property the file defines in `<properties>`, and
  `${project.*}` / `${pom.*}` / `${project.parent.*}`, are expanded once;
  nothing from another file is. Without both coordinates the project is
  `<file>::project`. The project declares an alias
  `maven:<groupId>:<artifactId>` of its identity, capped at Strong.
- **Parent** (`inheritance`, at most Strong). Maven uses the POM at
  `<relativePath>` (default `../pom.xml`; a directory means its `pom.xml`)
  if its coordinates are the parent's, else the artifact from a
  repository. The reference names `<that file>::<groupId>:<artifactId>` —
  path and coordinates at once — and the file declares a *fallback* alias
  (`AliasDraft.Fallback`) of that identity to `maven:<groupId>:<artifactId>`:
  a POM at the path with other coordinates, or none, leaves the parent to
  the repository's projects of those coordinates, else outside the
  repository. An empty `<relativePath/>`, and a path leaving the
  repository, name the coordinates only. The version is not compared,
  hence Strong.
- **Modules** (`<modules><module>`, Maven 4 `<subprojects><subproject>`,
  in profiles too): a `resource_link` with path evidence to
  `<module>/pom.xml` (or the `.xml` file it names), Exact; a module missing
  from the repository is Unresolved, one leaving it outside.
- **Dependencies** (`dependency`): dependencies, managed dependencies,
  plugins (groupId `org.apache.maven.plugins` by default), managed plugins
  and build extensions, `<file>::<section>::<groupId>:<artifactId>` (a
  repeat `#n`; in a profile `<file>::profile:<id>::<section>::…`), their
  version, type, classifier and scope in the signature. The project
  `depends_on` each dependency, plugin and extension of its build and
  `contains` the managed ones and a profile's. Each that is a dependency of
  the build — a managed one only with scope `import` — `depends_on`
  `maven:<groupId>:<artifactId>`: a project of the repository with those
  coordinates (Strong through its alias; several are Candidates) or
  outside the repository. Coordinates with a property the file does not
  define, or an unexpanded entity, are a dynamic name. Nothing is fetched.
  `maven_missing_coordinate` and `maven_duplicate_dependency` are warnings.

## Evaluation

- **Tests.** Unit tests of the three packages (identities, namespaces,
  positions with multi-byte text and CRLF, diagnostics, bounds, priority,
  determinism, stability, IDs, links, security, the POM), graph tests over
  `internal/languages/xml/testdata/repo` (a reactor of four modules, a
  parent found through its coordinates, an external parent, DocBook-like
  XML with namespaces, xml:id and XInclude, duplicated IDs, Java code
  beside the POM), `FuzzExtractXML` (seeded with POMs, namespaces, a
  billion-laughs DOCTYPE and broken documents; 1.13 million executions in
  180 s, then 2.55 million in 150 s after the last changes — its one
  finding, a harness that checked the file's own path as a name, is fixed
  and kept in `testdata/fuzz`) and
  `TestLinearTime` (one long line, nesting to the bound, many attributes:
  twice the input takes 1.5–1.7× the time).
- **Real files** (`TestRealFiles`, opt-in `ARK_XML_REAL_DIRS`, read only):
  the 799 `.xml` files of the Go module cache and of Ark (5.0 MB): no
  failure, every range within its file and every identity a name, 164299
  declarations, the slowest file (the HTTP/2 draft in x/net, 0.4 MB) 39 ms.
  The only diagnostics are 318 `xml_duplicate_attribute` in the Chroma
  lexer definitions, which do repeat an attribute
  (`<push state="a" state="b"/>`): genuine, since XML forbids it, though
  `encoding/xml` accepts it. Delve's TeamCity `pom.xml`: the project
  `Delve:Delve_dsl`, four dependencies and two plugins (outside the
  repository, versions from `${kotlin.version}` in the signature), its
  parent `configs-dsl-kotlin-parent` outside the repository. Indexing the
  353 XML files of Chroma: 69141 declarations, 3.4 s with compilation.
- **Mutation testing.** 30 mutants of the three packages (entity
  placeholders, the bounds, duplicate attributes, recovery, external
  identifiers, attribute bytes, ordinals, namespace labels, IdentityOnly,
  ID caps, href caps, links leaving the repository, xml:base, containers
  of references, parent fallback, coordinate caps, the default plugin
  group, managed dependencies, properties, modules leaving the repository,
  breadth-first priority, unprefixed attributes, relativePath directories,
  path evidence of the parent, identities of long paths, text bounds,
  namespace declarations, default namespaces, module locations): 29
  killed; one equivalent (the one-line check of a reference's target
  identity: every part of it is checked before).

## Remaining work

- IDREF / IDREFS by a schema or DTD (Ark reads neither), XPointer schemes
  other than a shorthand fragment, `xml:base`.
- Maven: properties from the parent or from profiles, the parent's version
  against the POM at the path, `${revision}` CI-friendly versions,
  dependency `<exclusions>`, plugin dependencies; Gradle.
- Vocabularies beyond the POM (Spring bean references, Android resources,
  MSBuild), and `.xsd`, `.xsl`, `.svg` files.
