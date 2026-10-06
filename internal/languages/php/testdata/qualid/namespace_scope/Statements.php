<?php
namespace A2;

use X\Foo;

class M1
{
    public function f() { return Foo::m(); }
}

namespace B2;

use Y\Foo;

class M2
{
    public function f() { return Foo::m(); }
}
