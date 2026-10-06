<?php
namespace App\Http;

class C
{
    public function a() { return \App\Services\Foo::make(); }

    public function b(\App\Services\Foo $f) { return $f->run(); }

    public function c() { return new \App\Services\Foo(); }
}
