// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Decred
{
    internal static class Errors
    {
        public static ArgumentOutOfRangeException RequireNonNegative(string paramName) =>
            new ArgumentOutOfRangeException(paramName, "Non-negative number required.");
    }
}