<?php
namespace App\Http;

use App\Services\Contract;
use App\Services\Foo;
use App\Services\LogsActivity;

class C
{
    public function a() { return Foo::make(); }

    public function b(Foo $f) { return $f->run(); }

    public function c() { return new Foo(); }
}

class D extends Foo
{
}

class E implements Contract
{
    public function run() {}
}

class F
{
    use LogsActivity;
}
