// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin;
using Xunit;

namespace Paymetheus.Tests.Bitcoin
{
    public static class AmountTests
    {
        [Theory]
        [InlineData((long)5e7, "0.5       ")]
        [InlineData((long)-5e7, "-0.5       ")]
        public static void FormatTests(long satoshis, string expected)
        {
            var amount = (Amount)satoshis;
            Assert.Equal(expected, amount.ToString());
        }
    }
}
