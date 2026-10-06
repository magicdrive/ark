<?php
namespace A {
    use X\Foo;

    class C1
    {
        public function f() { return Foo::m(); }
    }
}

namespace B {
    use Y\Foo;

    class C2
    {
        public function f() { return Foo::m(); }
    }
}
