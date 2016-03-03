// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using System;

namespace Paymetheus.Decred
{
    public class EncodingException : Exception
    {
        private const string messageFormat = "Operation uses or creates an invalid encoding for type {0}";
        private const string messageReasonFormat = messageFormat + ": {1}";

        public EncodingException(Type resultType, ByteCursor cursor, string reason)
            : base(string.Format(messageReasonFormat, resultType, reason))
        {
            ResultType = resultType;
            Cursor = cursor;
        }

        public EncodingException(Type resultType, ByteCursor cursor, Exception innerException)
            : base(string.Format(messageFormat, resultType), innerException)
        {
            ResultType = resultType;
            Cursor = cursor;
        }

        public Type ResultType { get; }
        public ByteCursor Cursor { get; }
    }
}
