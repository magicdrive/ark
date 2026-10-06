<?php
namespace App\Http;

use App\Services\Foo;

class C
{
    public function a() { return Foo::make(); }

    public function b(Foo $f) { return $f->run(); }

    public function c() { return new Foo(); }
}
