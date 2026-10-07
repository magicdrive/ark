<?php

namespace App\Http;

use App\Services\LoginScreenPolicy;

// Laravel 6 style untyped property: the call is only a candidate (it could be
// Clinic::showsSsoButton as far as the index can prove), so it is no edge.
class LoginController
{
    private $policy;

    public function __construct(LoginScreenPolicy $policy)
    {
        $this->policy = $policy;
    }

    public function showLoginForm()
    {
        return $this->policy->showsSsoButton();
    }
}
