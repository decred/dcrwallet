// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin.Util;
using System;
using System.Runtime.Serialization;

namespace Paymetheus.Bitcoin
{
    [Serializable]
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

        protected EncodingException(SerializationInfo info, StreamingContext context)
            : base(info, context)
        {
            ResultType = (Type)info.GetValue(nameof(ResultType), typeof(Type));
            Cursor = (ByteCursor)info.GetValue(nameof(Cursor), typeof(ByteCursor));
        }

        public Type ResultType { get; }
        public ByteCursor Cursor { get; }

        public override void GetObjectData(SerializationInfo info, StreamingContext context)
        {
            base.GetObjectData(info, context);
            info.AddValue(nameof(ResultType), ResultType);
            info.AddValue(nameof(Cursor), Cursor);
        }
    }
}
