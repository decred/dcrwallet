// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.ComponentModel;
using System.Runtime.CompilerServices;

namespace Paymetheus
{
    sealed class ButtonCommand : DelegateCommand, INotifyPropertyChanged
    {
        public ButtonCommand(string label, Action action) : base(action)
        {
            _buttonLabel = label;
        }

        private string _buttonLabel;
        public string ButtonLabel
        {
            get { return _buttonLabel; }
            set { _buttonLabel = value; RaisePropertyChanged(); }
        }

        public event PropertyChangedEventHandler PropertyChanged;

        void RaisePropertyChanged([CallerMemberName] string propertyName = null)
        {
            PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
        }
    }
}
