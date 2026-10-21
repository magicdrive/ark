// The javac oracle of internal/languages/java (TestOracle_Javac): for every
// name javac resolves in a set of sources compiled together, the declaration
// it denotes.
//
// usage: java docs/scripts/oracle/java-javac.java <project-dir> <out.tsv> <root>...
//
//   <project-dir>  the directory Ark indexes; paths are written relative to it
//   <out.tsv>      the oracle, written anew
//   <root>...      source roots (relative to <project-dir>) whose .java files
//                  are compiled together — one module, or one flavor of one
//                  (a type declared twice in one compilation is an error)
//
// The files are attributed in batches (ARK_JAVA_ORACLE_BATCH, default all at
// once), the roots on the source path. javac 21 fails an assertion on some
// erroneous code (a switch expression's yield whose type a missing
// dependency leaves unknown): a batch that fails is split in halves, and a
// file that fails alone is left out — counted on stderr, its names
// unverified.
//
// Run it with the java launcher of a JDK of the language level the sources
// need (measured with Temurin 21). javac parses and attributes the sources:
// annotation processing is off (-proc:none), no class file is written, and
// nothing else is run or fetched. ARK_JAVA_ORACLE_CLASSPATH, when set, is
// the class path (the sources' dependencies: jars the caller provides; the
// sources themselves are preferred to a class of the same name there). A
// type of a dependency javac does not have is unknown to it, and so are the
// names whose meaning depends on it — such names are erroneous and not
// written (the test counts them as unverified).
//
// One line per name, tab-separated:
//
//   file  line  column  kind  name  target
//
// column is the byte offset of the name in its line plus one (Ark's
// columns), kind is type, method, ctor or field, name the name as written
// (<init> for a constructor), and target
//
//   src:<file>:<first line>:<last line>:<identity>   a declaration of the sources
//   ext:<identity>                                   one javac knows otherwise (the JDK)
//   implicit:<identity>                              one the language declares implicitly
//
// Not written, being javac's error recovery rather than the language: a
// name javac attributes to an erroneous type, the method or constructor a
// call with an argument of erroneous type invokes (javac picks one anyway),
// and, in a file whose
// single-type import javac cannot resolve (a dependency's type), every name
// spelled like that import's simple name — javac then takes another type of
// the name, which the source does not mean.
//
// identity is a type's canonical name, or its owner's and the member's name
// (owner.name, owner.<init>); "" for a type without a canonical name (local,
// anonymous) and its members.
import com.sun.source.tree.*;
import com.sun.source.util.*;

import javax.lang.model.element.*;
import javax.tools.*;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;
import java.util.stream.*;

public class JavaOracle {
    public static void main(String[] args) throws Exception {
        if (args.length < 3) {
            System.err.println("usage: java java-javac.java <project-dir> <out.tsv> <root>...");
            System.exit(2);
        }
        Path project = Paths.get(args[0]).toAbsolutePath().normalize();
        Path out = Paths.get(args[1]);
        List<Path> files = new ArrayList<>();
        for (int i = 2; i < args.length; i++) {
            try (Stream<Path> s = Files.walk(project.resolve(args[i]))) {
                s.filter(p -> p.toString().endsWith(".java") && !p.getFileName().toString().equals("module-info.java"))
                 .sorted().forEach(files::add);
            }
        }
        // A root holding a module-info.java would make javac read the source
        // path as a module: its files are compiled, the root is not on it.
        String sourcePath = Arrays.stream(args, 2, args.length).map(r -> project.resolve(r))
                .filter(r -> !Files.exists(r.resolve("module-info.java")))
                .map(Path::toString).collect(Collectors.joining(File.pathSeparator));
        int batch = files.size();
        String b = System.getenv("ARK_JAVA_ORACLE_BATCH");
        if (b != null && !b.isEmpty()) batch = Integer.parseInt(b);
        int[] counts = new int[3]; // errors, files left out, batches
        try (PrintWriter w = new PrintWriter(Files.newBufferedWriter(out, StandardCharsets.UTF_8))) {
            for (int i = 0; i < files.size(); i += batch) {
                attribute(project, files.subList(i, Math.min(files.size(), i + batch)), sourcePath, w, counts);
            }
        }
        System.err.printf("javac oracle: %d files, %d errors, %d left out, %d batches%n", files.size(), counts[0], counts[1], counts[2]);
    }

    // attribute writes the names of files, compiled together with the
    // source path; on javac's own failure it splits them.
    static void attribute(Path project, List<Path> files, String sourcePath, PrintWriter w, int[] counts) throws IOException {
        JavaCompiler javac = ToolProvider.getSystemJavaCompiler();
        DiagnosticCollector<JavaFileObject> diags = new DiagnosticCollector<>();
        StandardJavaFileManager fm = javac.getStandardFileManager(diags, null, StandardCharsets.UTF_8);
        Iterable<? extends JavaFileObject> units = fm.getJavaFileObjectsFromPaths(files);
        List<String> opts = new ArrayList<>(List.of("-proc:none", "-implicit:none", "-Xlint:none", "-nowarn", "-encoding", "UTF-8",
                "-XDshould-stop.ifError=ATTR", "-Xmaxerrs", "100000", "-sourcepath", sourcePath, "-Xprefer:source"));
        String cp = System.getenv("ARK_JAVA_ORACLE_CLASSPATH");
        if (cp != null && !cp.isEmpty()) {
            opts.add("-classpath");
            opts.add(cp);
        }
        JavacTask task = (JavacTask) javac.getTask(null, fm, diags, opts, null, units);
        Iterable<? extends CompilationUnitTree> cus;
        try {
            cus = task.parse();
            task.analyze();
        } catch (IllegalStateException | AssertionError | NullPointerException e) {
            if (files.size() == 1) {
                counts[1]++;
                System.err.println("left out (javac failed): " + files.get(0));
                return;
            }
            int half = files.size() / 2;
            attribute(project, files.subList(0, half), sourcePath, w, counts);
            attribute(project, files.subList(half, files.size()), sourcePath, w, counts);
            return;
        }
        counts[2]++;
        for (Diagnostic<?> d : diags.getDiagnostics()) {
            if (d.getKind() == Diagnostic.Kind.ERROR) counts[0]++;
        }
        Trees trees = Trees.instance(task);
        for (CompilationUnitTree cu : cus) {
            new Scanner(project, trees, cu, w).scan(cu, null);
        }
        w.flush();
    }

    static final class Scanner extends TreePathScanner<Void, Void> {
        final Path project;
        final Trees trees;
        final CompilationUnitTree cu;
        final PrintWriter w;
        final String text;
        final String file;
        final SourcePositions pos;
        final LineMap lines;

        Scanner(Path project, Trees trees, CompilationUnitTree cu, PrintWriter w) throws IOException {
            this.project = project;
            this.trees = trees;
            this.cu = cu;
            this.w = w;
            this.text = cu.getSourceFile().getCharContent(true).toString();
            this.file = rel(Paths.get(cu.getSourceFile().toUri()));
            this.pos = trees.getSourcePositions();
            this.lines = cu.getLineMap();
        }

        String rel(Path p) {
            return project.relativize(p.toAbsolutePath().normalize()).toString().replace('\\', '/');
        }

        // Simple names of the file's single-type imports javac cannot resolve.
        final Set<String> unresolvedImports = new HashSet<>();

        @Override public Void visitCompilationUnit(CompilationUnitTree t, Void v) {
            for (ImportTree imp : t.getImports()) {
                if (imp.isStatic() || !(imp.getQualifiedIdentifier() instanceof MemberSelectTree ms)) continue;
                String simple = ms.getIdentifier().toString();
                if (simple.equals("*")) continue;
                Element e = trees.getElement(new TreePath(new TreePath(getCurrentPath(), imp), ms));
                if (e == null || e.asType() == null || e.asType().getKind() == javax.lang.model.type.TypeKind.ERROR) {
                    unresolvedImports.add(simple);
                }
            }
            return super.visitCompilationUnit(t, v);
        }

        @Override public Void visitImport(ImportTree t, Void v) { return null; }
        @Override public Void visitPackage(PackageTree t, Void v) { return null; }

        @Override public Void visitIdentifier(IdentifierTree t, Void v) {
            long start = pos.getStartPosition(cu, t);
            String name = t.getName().toString();
            if (!name.equals("this") && !name.equals("super")) {
                record(getCurrentPath(), start, name);
            }
            return super.visitIdentifier(t, v);
        }

        // The method selects of calls javac chose an overload for by
        // arguments it could not type.
        final Set<Tree> guessed = Collections.newSetFromMap(new IdentityHashMap<>());

        boolean erroneousArgument(List<? extends ExpressionTree> args) {
            for (ExpressionTree a : args) {
                javax.lang.model.type.TypeMirror tm = trees.getTypeMirror(new TreePath(getCurrentPath(), a));
                if (tm == null || tm.getKind() == javax.lang.model.type.TypeKind.ERROR) return true;
            }
            return false;
        }

        @Override public Void visitMethodInvocation(MethodInvocationTree t, Void v) {
            if (erroneousArgument(t.getArguments())) guessed.add(t.getMethodSelect());
            // this(...) and super(...): the constructor invoked, at the keyword.
            if (t.getMethodSelect() instanceof IdentifierTree id) {
                String n = id.getName().toString();
                if (n.equals("this") || n.equals("super")) {
                    Element ctor = trees.getElement(new TreePath(getCurrentPath(), id));
                    if (ctor != null && ctor.getKind() == ElementKind.CONSTRUCTOR && !erroneousArgument(t.getArguments())) {
                        emit(pos.getStartPosition(cu, id), "ctor", "<init>", ctor);
                    }
                }
            }
            return super.visitMethodInvocation(t, v);
        }

        @Override public Void visitMemberSelect(MemberSelectTree t, Void v) {
            long end = pos.getEndPosition(cu, t);
            String name = t.getIdentifier().toString();
            if (end > 0 && !name.equals("class") && !name.equals("this") && !name.equals("super")) {
                record(getCurrentPath(), end - name.length(), name);
            }
            return super.visitMemberSelect(t, v);
        }

        @Override public Void visitMemberReference(MemberReferenceTree t, Void v) {
            long end = pos.getEndPosition(cu, t);
            String name = t.getName().toString();
            if (end > 0 && !name.equals("<init>")) {
                record(getCurrentPath(), end - name.length(), name);
            }
            return super.visitMemberReference(t, v);
        }

        @Override public Void visitNewClass(NewClassTree t, Void v) {
            Element ctor = erroneousArgument(t.getArguments()) ? null : trees.getElement(getCurrentPath());
            Tree clazz = t.getIdentifier();
            while (clazz instanceof ParameterizedTypeTree pt) clazz = pt.getType();
            while (clazz instanceof AnnotatedTypeTree at) clazz = at.getUnderlyingType();
            long start = -1;
            if (clazz instanceof IdentifierTree id) start = pos.getStartPosition(cu, id);
            else if (clazz instanceof MemberSelectTree ms) start = pos.getEndPosition(cu, ms) - ms.getIdentifier().length();
            if (start >= 0 && ctor != null && ctor.getKind() == ElementKind.CONSTRUCTOR && t.getEnclosingExpression() == null) {
                emit(start, "ctor", "<init>", ctor);
            }
            return super.visitNewClass(t, v);
        }

        void record(TreePath path, long start, String name) {
            if (start < 0 || unresolvedImports.contains(name) || guessed.contains(path.getLeaf())) return;
            Element e = trees.getElement(path);
            if (e == null || e.asType() == null || e.asType().getKind() == javax.lang.model.type.TypeKind.ERROR) return;
            String kind;
            switch (e.getKind()) {
                case CLASS, INTERFACE, ENUM, RECORD, ANNOTATION_TYPE -> kind = "type";
                case METHOD -> kind = "method";
                case CONSTRUCTOR -> kind = "ctor";
                case FIELD, ENUM_CONSTANT -> kind = "field";
                default -> { return; }
            }
            if (kind.equals("ctor")) name = "<init>";
            emit(start, kind, name, e);
        }

        void emit(long start, String kind, String name, Element e) {
            if (identity(e).contains("<any")) return; // an erroneous type
            long line = lines.getLineNumber(start);
            long lineStart = lines.getStartPosition(line);
            int col = text.substring((int) lineStart, (int) start).getBytes(StandardCharsets.UTF_8).length + 1;
            w.printf("%s\t%d\t%d\t%s\t%s\t%s%n", file, line, col, kind, name, target(e));
        }

        String identity(Element e) {
            if (e instanceof TypeElement te) {
                // No canonical name inside a local or an anonymous class.
                for (Element c = te; c instanceof TypeElement ct; c = c.getEnclosingElement()) {
                    if (ct.getNestingKind() == NestingKind.LOCAL || ct.getNestingKind() == NestingKind.ANONYMOUS) return "";
                }
                for (Element c = te.getEnclosingElement(); c != null && !(c instanceof PackageElement); c = c.getEnclosingElement()) {
                    if (c instanceof ExecutableElement) return "";
                }
                return te.getQualifiedName().toString();
            }
            Element owner = e.getEnclosingElement();
            String o = owner == null ? "" : identity(owner);
            if (o.isEmpty()) return "";
            return o + "." + (e.getKind() == ElementKind.CONSTRUCTOR ? "<init>" : e.getSimpleName().toString());
        }

        String target(Element e) {
            TreePath decl = trees.getPath(e);
            String id = identity(e);
            if (decl == null) {
                // Declared by a class file, or implicitly by a source type.
                TypeElement top = topLevel(e);
                if (top != null && trees.getPath(top) != null) return "implicit:" + id;
                return "ext:" + id;
            }
            CompilationUnitTree dcu = decl.getCompilationUnit();
            Tree leaf = decl.getLeaf();
            long s = trees.getSourcePositions().getStartPosition(dcu, leaf);
            long en = trees.getSourcePositions().getEndPosition(dcu, leaf);
            if (s < 0 || en < 0) {
                return "implicit:" + id; // a member the compiler generated (a record's accessor)
            }
            LineMap lm = dcu.getLineMap();
            return String.format("src:%s:%d:%d:%s", rel(Paths.get(dcu.getSourceFile().toUri())), lm.getLineNumber(s), lm.getLineNumber(en), id);
        }

        static TypeElement topLevel(Element e) {
            TypeElement top = null;
            for (Element c = e; c != null; c = c.getEnclosingElement()) {
                if (c instanceof TypeElement te) top = te;
                if (c instanceof PackageElement) break;
            }
            return top;
        }
    }
}
