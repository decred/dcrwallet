// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using System.Collections.Generic;
using Xunit;

namespace Paymetheus.Tests.Decred
{
    public static class TransactionTests
    {
        static class TestTransactions
        {
            public static readonly Transaction EmptyTransaction =
                new Transaction(Transaction.SupportedVersion, new Transaction.Input[0], new Transaction.Output[0], 0, 0);
        }

        public static IEnumerable<object[]> SerializeSizeTheories()
        {
            return new[]
            {
                new object[] { TestTransactions.EmptyTransaction, 10},
            };
        }

        [Theory]
        [MemberData(nameof(SerializeSizeTheories))]
        public static void SerializeSizes(Transaction tx, int expectedSerializeSize)
        {
            var serializeSize = tx.SerializeSize;
            Assert.Equal(expectedSerializeSize, serializeSize);
        }
    }
}
