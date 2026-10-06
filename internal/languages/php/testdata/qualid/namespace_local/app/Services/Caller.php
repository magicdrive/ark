<?php
namespace App\Services;

class Caller
{
    public function a() { return Foo::make(); }

    public function b(Foo $f) { return $f->run(); }

    public function c() { return new Foo(); }

    public function d() { return namespace\Foo::make(); }
}
