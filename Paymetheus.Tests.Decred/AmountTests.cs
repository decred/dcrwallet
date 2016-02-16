// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Xunit;

namespace Paymetheus.Tests.Decred
{
    public static class AmountTests
    {
        [Theory]
        [InlineData((long)5e7, "0.5       ")]
        [InlineData((long)-5e7, "-0.5       ")]
        public static void FormatTests(long atoms, string expected)
        {
            var amount = (Amount)atoms;
            Assert.Equal(expected, amount.ToString());
        }
    }
}
